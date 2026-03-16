package e2e

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/robfig/cron/v3"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/sdk"
)

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

func loadPlaybookFixture(path string) v1.Playbook {
	content, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())

	var pb v1.Playbook
	Expect(yaml.Unmarshal(content, &pb)).To(Succeed())

	if pb.UID == "" {
		pb.UID = types.UID(uuid.NewString())
	}
	return pb
}

func grantArtifactAccess(pb *v1.Playbook) {
	perm := &v1.Permission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("allow-%s-artifacts", pb.Name),
			Namespace: pb.Namespace,
			UID:       types.UID(uuid.NewString()),
		},
		Spec: v1.PermissionSpec{
			Description: fmt.Sprintf("allow %s/%s to read artifacts connection", pb.Namespace, pb.Name),
			Subject:     v1.PermissionSubject{Playbook: fmt.Sprintf("%s/%s", pb.Namespace, pb.Name)},
			Actions:     []string{"read"},
			Object: v1.PermissionObject{
				Selectors: dutyRBAC.Selectors{
					Connections: []dutyTypes.ResourceSelector{{Name: "artifacts", Namespace: "default"}},
				},
			},
		},
	}
	Expect(db.PersistPermissionFromCRD(DefaultContext, perm)).To(Succeed())
	Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
}

var _ = ginkgo.Describe("Report action with email delivery", ginkgo.Ordered, func() {
	var (
		smtpSrv     *gosmtp.Server
		smtpBackend *reportSMTPBackend
		smtpPort    int
		smtpConn    *v1.Connection
		viewRef     string
	)

	ginkgo.BeforeAll(func() {
		smtpBackend = &reportSMTPBackend{}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		smtpPort = listener.Addr().(*net.TCPAddr).Port

		smtpSrv = gosmtp.NewServer(smtpBackend)
		smtpSrv.Domain = "localhost"
		smtpSrv.AllowInsecureAuth = true
		go func() { defer ginkgo.GinkgoRecover(); _ = smtpSrv.Serve(listener) }()

		smtpConn = &v1.Connection{
			TypeMeta: metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Connection"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "system", Namespace: "mc",
				UID: types.UID(uuid.NewString()),
			},
			Spec: v1.ConnectionSpec{
				SMTP: &v1.ConnectionSMTP{
					Host:        "127.0.0.1",
					Port:        smtpPort,
					FromAddress: "reports@flanksource.com",
					FromName:    "Mission Control Reports",
					Auth:        v1.SMTPAuthNone,
					Encryption:  v1.EncryptionNone,
					ToAddresses: []string{"reports@flanksource.com"},
				},
			},
		}
		Expect(db.PersistConnectionFromCRD(DefaultContext, smtpConn)).To(Succeed())
		mail.FlushSMTPCache()

		viewRef = fmt.Sprintf("%s/%s", dummy.PodView.Namespace, dummy.PodView.Name)
	})

	ginkgo.AfterAll(func() {
		mail.FlushSMTPCache()
		if smtpSrv != nil {
			_ = smtpSrv.Close()
		}
		if smtpConn != nil {
			_ = DefaultContext.DB().
				Where("id = ? AND deleted_at IS NULL", string(smtpConn.UID)).
				Delete(&models.Connection{}).Error
		}
	})

	ginkgo.It("generates HTML + PDF report and emails with PDF attachment", func() {
		smtpBackend.clear()

		pb := loadPlaybookFixture("testdata/report-email-playbook.yaml")
		Expect(db.PersistPlaybookFromCRD(DefaultContext, &pb)).To(Succeed())
		grantArtifactAccess(&pb)

		smtpURL := fmt.Sprintf("%s?ToAddresses=%s", api.SystemSMTP, url.QueryEscape("test-recipient@flanksource.com"))
		run, err := client.Run(sdk.RunParams{
			ID:       pb.UID,
			ConfigID: dummy.LogisticsAPIPodConfig.ID,
			Params: map[string]string{
				"view":     viewRef,
				"smtp_url": smtpURL,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		var pbRun models.PlaybookRun
		Expect(DefaultContext.DB().Where("id = ?", run.RunID).First(&pbRun).Error).To(Succeed())

		completedRun := waitFor(DefaultContext, &pbRun)
		Expect(completedRun.Status).To(Equal(models.PlaybookRunStatusCompleted), "%v", completedRun)

		var actions []models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("playbook_run_id = ?", pbRun.ID).Order("start_time").Find(&actions).Error).To(Succeed())
		Expect(actions).To(HaveLen(3))

		// Verify HTML report action
		htmlAction := actions[0]
		Expect(htmlAction.Name).To(Equal("generate-report"))
		Expect(htmlAction.Status).To(Equal(models.PlaybookActionStatusCompleted))
		Expect(htmlAction.Result["format"]).To(Equal("html"))
		Expect(htmlAction.Result).ToNot(HaveKey("rendered"))

		// Verify artifacts were created for HTML report
		var htmlArtifacts []models.Artifact
		Expect(DefaultContext.DB().Where("playbook_run_action_id = ?", htmlAction.ID).Find(&htmlArtifacts).Error).To(Succeed())
		Expect(htmlArtifacts).To(HaveLen(1))
		Expect(htmlArtifacts[0].ContentType).To(Equal("text/html"))

		// Verify PDF report action
		pdfAction := actions[1]
		Expect(pdfAction.Name).To(Equal("generate-pdf"))
		Expect(pdfAction.Status).To(Equal(models.PlaybookActionStatusCompleted))
		Expect(pdfAction.Result["format"]).To(Equal("pdf"))
		Expect(pdfAction.Result).ToNot(HaveKey("rendered"))

		// Verify artifacts were created for PDF report
		var pdfArtifacts []models.Artifact
		Expect(DefaultContext.DB().Where("playbook_run_action_id = ?", pdfAction.ID).Find(&pdfArtifacts).Error).To(Succeed())
		Expect(pdfArtifacts).To(HaveLen(1))
		Expect(pdfArtifacts[0].ContentType).To(Equal("application/pdf"))

		// Verify notification action
		notifAction := actions[2]
		Expect(notifAction.Name).To(Equal("send-email"))
		Expect(notifAction.Status).To(Equal(models.PlaybookActionStatusCompleted))

		// Verify email was received with attachment
		Eventually(func() int {
			return len(smtpBackend.getMessages())
		}, 10*time.Second, 200*time.Millisecond).Should(BeNumerically(">=", 1))

		msgs := smtpBackend.getMessages()
		emailBody := string(msgs[len(msgs)-1].Data)
		Expect(emailBody).To(ContainSubstring("Pod Report"))
		Expect(emailBody).To(ContainSubstring("report.pdf"))
		Expect(emailBody).To(ContainSubstring("application/pdf"))
	})

	ginkgo.It("runs an inline config report action", func() {
		pb := loadPlaybookFixture("testdata/inline-report-playbook.yaml")
		Expect(db.PersistPlaybookFromCRD(DefaultContext, &pb)).To(Succeed())
		grantArtifactAccess(&pb)

		run, err := client.Run(sdk.RunParams{
			ID:       pb.UID,
			ConfigID: dummy.LogisticsAPIPodConfig.ID,
		})
		Expect(err).ToNot(HaveOccurred())

		var pbRun models.PlaybookRun
		Expect(DefaultContext.DB().Where("id = ?", run.RunID).First(&pbRun).Error).To(Succeed())

		completedRun := waitFor(DefaultContext, &pbRun)
		Expect(completedRun.Status).To(Equal(models.PlaybookRunStatusCompleted), "%v", completedRun)

		var actions []models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("playbook_run_id = ?", pbRun.ID).Find(&actions).Error).To(Succeed())
		Expect(actions).To(HaveLen(1))

		Expect(actions[0].Status).To(Equal(models.PlaybookActionStatusCompleted))
		Expect(actions[0].Result["format"]).To(Equal("json"))
		Expect(actions[0].Result).ToNot(HaveKey("rendered"))

		var inlineArtifacts []models.Artifact
		Expect(DefaultContext.DB().Where("playbook_run_action_id = ?", actions[0].ID).Find(&inlineArtifacts).Error).To(Succeed())
		Expect(inlineArtifacts).To(HaveLen(1))
		Expect(inlineArtifacts[0].ContentType).To(Equal("application/json"))
	})

	ginkgo.It("runs a scheduled playbook", ginkgo.Label("slow"), func() {
		pb := loadPlaybookFixture("testdata/scheduled-report-playbook.yaml")
		Expect(db.PersistPlaybookFromCRD(DefaultContext, &pb)).To(Succeed())
		grantArtifactAccess(&pb)

		playbookModel, err := pb.ToModel()
		Expect(err).ToNot(HaveOccurred())

		// Inject the view param into the schedule parameters.
		Expect(DefaultContext.DB().Exec(
			`UPDATE playbooks SET spec = jsonb_set(spec, '{on,schedule,0,parameters,view}', to_jsonb(?::text)) WHERE id = ?`,
			viewRef, playbookModel.ID,
		).Error).To(Succeed())

		testScheduler := cron.New()
		testScheduler.Start()
		defer testScheduler.Stop()

		Expect(playbook.SyncPlaybookSchedulesForTest(DefaultContext, testScheduler)).To(Succeed())
		Expect(testScheduler.Entries()).ToNot(BeEmpty(), "expected cron entries to be registered")

		// Wait for the cron to fire and create a run
		Eventually(func() int64 {
			var count int64
			DefaultContext.DB().Model(&models.PlaybookRun{}).Where("playbook_id = ?", playbookModel.ID).Count(&count)
			return count
		}, 30*time.Second, time.Second).Should(BeNumerically(">=", 1))

		// Clean up
		_ = DefaultContext.DB().Model(&models.Playbook{}).
			Where("id = ?", playbookModel.ID).
			Update("deleted_at", duty.Now()).Error
		_ = playbook.SyncPlaybookSchedulesForTest(DefaultContext, testScheduler)
	})
})
