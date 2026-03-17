package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
)

type fixtureContext struct {
	Fixture  *playbookFixture
	Path     string
	Vars     map[string]string
	CelEnv   map[string]any
	Playbook *v1.Playbook
}

type setupHandler interface {
	Labels(setup fixtureSetup) []string
	Handle(fctx *fixtureContext)
	Cleanup()
}

var allSetupHandlers = []setupHandler{
	&lokiHandler{},
	&opensearchHandler{},
	&smtpHandler{},
	&facetHandler{},
	&connectionsHandler{},
	&permissionsHandler{},
}

// --- SMTP types ---

type reportSMTPBackend struct {
	mu       sync.Mutex
	messages []reportSMTPMessage
}

type reportSMTPMessage struct {
	From string
	To   []string
	Data []byte
}

func (b *reportSMTPBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &reportSMTPSession{backend: b}, nil
}

func (b *reportSMTPBackend) getMessages() []reportSMTPMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]reportSMTPMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

func (b *reportSMTPBackend) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = nil
}

type reportSMTPSession struct {
	backend *reportSMTPBackend
	from    string
	to      []string
}

func (s *reportSMTPSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}
func (s *reportSMTPSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}
func (s *reportSMTPSession) Reset()        { s.from = ""; s.to = nil }
func (s *reportSMTPSession) Logout() error { return nil }

func (s *reportSMTPSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.backend.mu.Lock()
	s.backend.messages = append(s.backend.messages, reportSMTPMessage{From: s.from, To: s.to, Data: data})
	s.backend.mu.Unlock()
	return nil
}

// --- SMTP handler ---

type smtpHandler struct {
	once    sync.Once
	srv     *gosmtp.Server
	backend *reportSMTPBackend
	conn    *v1.Connection
}

func (h *smtpHandler) Labels(_ fixtureSetup) []string { return nil }

func (h *smtpHandler) Handle(fctx *fixtureContext) {
	if !fctx.Fixture.Setup.SMTP {
		return
	}

	h.once.Do(func() {
		h.backend = &reportSMTPBackend{}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		port := listener.Addr().(*net.TCPAddr).Port

		h.srv = gosmtp.NewServer(h.backend)
		h.srv.Domain = "localhost"
		h.srv.AllowInsecureAuth = true
		go func() { defer ginkgo.GinkgoRecover(); _ = h.srv.Serve(listener) }()

		h.conn = &v1.Connection{
			TypeMeta: metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Connection"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "system", Namespace: "mc",
				UID: types.UID(uuid.NewString()),
			},
			Spec: v1.ConnectionSpec{
				SMTP: &v1.ConnectionSMTP{
					Host:        "127.0.0.1",
					Port:        port,
					FromAddress: "reports@flanksource.com",
					FromName:    "Mission Control Reports",
					Auth:        v1.SMTPAuthNone,
					Encryption:  v1.EncryptionNone,
					ToAddresses: []string{"reports@flanksource.com"},
				},
			},
		}
		Expect(db.PersistConnectionFromCRD(DefaultContext, h.conn)).To(Succeed())
		mail.FlushSMTPCache()
	})

	h.backend.clear()

	smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape("test-recipient@flanksource.com"))
	fctx.Vars["smtpURL"] = smtpURL

	fctx.CelEnv["_smtpCollect"] = func() {
		if len(fctx.Fixture.Assertions) == 0 {
			return
		}
		Eventually(func() int {
			return len(h.backend.getMessages())
		}, 10*time.Second, 200*time.Millisecond).Should(BeNumerically(">=", 1))

		msgs := h.backend.getMessages()
		emailBody := string(msgs[len(msgs)-1].Data)
		fctx.CelEnv["emails"] = []map[string]any{
			{"body": emailBody},
		}
	}
}

func (h *smtpHandler) Cleanup() {
	mail.FlushSMTPCache()
	if h.srv != nil {
		_ = h.srv.Close()
	}
	if h.conn != nil {
		_ = DefaultContext.DB().
			Where("id = ? AND deleted_at IS NULL", string(h.conn.UID)).
			Delete(&models.Connection{}).Error
	}
}

// --- Facet handler ---

type facetHandler struct{}

func (h *facetHandler) Labels(_ fixtureSetup) []string { return nil }

func (h *facetHandler) Handle(fctx *fixtureContext) {
	if !fctx.Fixture.Setup.Facet {
		return
	}
	facetPort, err := facetContainer.GetPort("3010")
	Expect(err).ToNot(HaveOccurred())
	fctx.Vars["facetURL"] = fmt.Sprintf("http://localhost:%s", facetPort)
}

func (h *facetHandler) Cleanup() {}

// --- Loki handler ---

type lokiHandler struct {
	once sync.Once
}

func (h *lokiHandler) Labels(setup fixtureSetup) []string {
	if setup.Loki {
		return []string{"external"}
	}
	return nil
}

func (h *lokiHandler) Handle(fctx *fixtureContext) {
	if !fctx.Fixture.Setup.Loki {
		return
	}
	h.once.Do(func() {
		content, err := os.ReadFile("setup/seed-loki.json")
		Expect(err).To(BeNil())

		baseTime := time.Now().Add(-5 * time.Minute)
		updated := string(content)
		updated = strings.ReplaceAll(updated, "{{TIMESTAMP_1}}", fmt.Sprintf("%d", baseTime.UnixNano()))
		updated = strings.ReplaceAll(updated, "{{TIMESTAMP_2}}", fmt.Sprintf("%d", baseTime.Add(1*time.Second).UnixNano()))
		updated = strings.ReplaceAll(updated, "{{TIMESTAMP_3}}", fmt.Sprintf("%d", baseTime.Add(2*time.Second).UnixNano()))

		pushEndpoint, err := url.JoinPath(lokiEndpoint, "loki/api/v1/push")
		Expect(err).To(BeNil())

		resp, err := http.NewClient().R(DefaultContext).Header("Content-Type", "application/json").Post(pushEndpoint, updated)
		Expect(err).To(BeNil())
		Expect(resp.IsOK()).To(BeTrue())

		waitForLokiLogs()
	})
}

func (h *lokiHandler) Cleanup() {}

// --- OpenSearch handler ---

type opensearchHandler struct {
	once sync.Once
}

func (h *opensearchHandler) Labels(setup fixtureSetup) []string {
	if setup.OpenSearch {
		return []string{"external"}
	}
	return nil
}

func (h *opensearchHandler) Handle(fctx *fixtureContext) {
	if !fctx.Fixture.Setup.OpenSearch {
		return
	}
	h.once.Do(func() {
		content, err := os.ReadFile("setup/seed-opensearch.json")
		Expect(err).To(BeNil())

		bulkEndpoint, err := url.JoinPath(openSearchEndpoint, "_bulk")
		Expect(err).To(BeNil())

		resp, err := http.NewClient().R(DefaultContext).Header("Content-Type", "application/json").Post(bulkEndpoint, content)
		Expect(err).To(BeNil())

		bodyBytes, err := io.ReadAll(resp.Body)
		Expect(err).To(BeNil())
		resp.Body.Close()

		Expect(resp.IsOK()).To(BeTrue(), "OpenSearch bulk insert failed: %s %s", resp.Response.Status, string(bodyBytes))

		var bulkResponse map[string]any
		Expect(json.Unmarshal(bodyBytes, &bulkResponse)).To(BeNil())
		Expect(bulkResponse["errors"]).To(Equal(false), "OpenSearch bulk insert had errors: %+v", bulkResponse)

		refreshEndpoint, err := url.JoinPath(openSearchEndpoint, "k8s-logs/_refresh")
		Expect(err).To(BeNil())
		refreshResp, err := http.NewClient().R(DefaultContext).Post(refreshEndpoint, "")
		Expect(err).To(BeNil())
		Expect(refreshResp.IsOK()).To(BeTrue(), "OpenSearch refresh failed")
	})
}

func (h *opensearchHandler) Cleanup() {}

// --- Connections handler ---

type connectionsHandler struct{}

func (h *connectionsHandler) Labels(_ fixtureSetup) []string { return nil }

func (h *connectionsHandler) Handle(fctx *fixtureContext) {
	for _, ref := range fctx.Fixture.Setup.Connections {
		var conn v1.Connection
		Expect(ref.resolve(fctx.Path, &conn)).To(Succeed())
		if conn.UID == "" {
			conn.UID = types.UID(uuid.NewString())
		}
		Expect(db.PersistConnectionFromCRD(DefaultContext, &conn)).To(Succeed())
	}
}

func (h *connectionsHandler) Cleanup() {}

// --- Permissions handler ---

type permissionsHandler struct{}

func (h *permissionsHandler) Labels(_ fixtureSetup) []string { return nil }

func (h *permissionsHandler) Handle(fctx *fixtureContext) {
	if len(fctx.Fixture.Setup.Permissions) == 0 {
		return
	}

	playbookRef := fmt.Sprintf("%s/%s", fctx.Playbook.Namespace, fctx.Playbook.Name)

	for _, ref := range fctx.Fixture.Setup.Permissions {
		if ref.Ref != "" {
			content, err := os.ReadFile(filepath.Join(filepath.Dir(fctx.Path), ref.Ref))
			Expect(err).ToNot(HaveOccurred())
			expanded := strings.ReplaceAll(string(content), "{{playbookRef}}", playbookRef)

			var perm v1.Permission
			Expect(yaml.Unmarshal([]byte(expanded), &perm)).To(Succeed())
			if perm.UID == "" {
				perm.UID = types.UID(uuid.NewString())
			}
			Expect(db.PersistPermissionFromCRD(DefaultContext, &perm)).To(Succeed())
		} else {
			var perm v1.Permission
			Expect(json.Unmarshal(ref.Inline, &perm)).To(Succeed())
			if perm.UID == "" {
				perm.UID = types.UID(uuid.NewString())
			}
			Expect(db.PersistPermissionFromCRD(DefaultContext, &perm)).To(Succeed())
		}
	}

	Expect(rbac.ReloadPolicy()).To(Succeed())
}

func (h *permissionsHandler) Cleanup() {}
