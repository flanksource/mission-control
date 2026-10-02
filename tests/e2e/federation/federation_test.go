package federation

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("Federated requests through the server", ginkgo.Ordered, ginkgo.ContinueOnFailure, func() {
	const (
		audience  = "https://mission-control.test"
		namespace = "fed-e2e"
		keyID     = "appx-key"
	)

	var (
		signingKey      *rsa.PrivateKey
		issuer          *httptest.Server
		allowed, denied models.ConfigItem
		echoPlaybook    models.Playbook
		otherPlaybook   models.Playbook
	)

	sign := func(key *rsa.PrivateKey, claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = keyID
		signed, err := token.SignedString(key)
		Expect(err).ToNot(HaveOccurred())
		return signed
	}

	tinaToken := func(tenant string, groups ...string) string {
		now := time.Now()
		return sign(signingKey, jwt.MapClaims{
			"iss":    issuer.URL,
			"aud":    audience,
			"sub":    "tina-123",
			"name":   "Tina",
			"email":  "tina@client.com",
			"tenant": tenant,
			"groups": groups,
			"iat":    now.Unix(),
			"exp":    now.Add(time.Minute).Unix(),
		})
	}

	operator := func() string {
		return tinaToken("a", "operators")
	}

	// request calls the server. token is a bearer token, or "admin" for basic auth as the admin.
	request := func(method, path, token string, body any) (int, []byte) {
		var reader io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
			reader = bytes.NewReader(b)
		}

		req, err := http.NewRequest(method, server.URL+path, reader)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if token == "admin" {
			req.SetBasicAuth(auth.AdminName, "admin")
		} else if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		return resp.StatusCode, out
	}

	configIDs := func(body []byte) []string {
		var rows []struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(body, &rows)).To(Succeed(), string(body))
		return lo.Map(rows, func(r struct {
			ID string `json:"id"`
		}, _ int) string {
			return r.ID
		})
	}

	ginkgo.BeforeAll(func() {
		ctx := DefaultContext

		var err error
		signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).ToNot(HaveOccurred())

		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer.URL, "jwks_uri": issuer.URL + "/jwks"})
		})
		mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA",
				"kid": keyID,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(signingKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(signingKey.E)).Bytes()),
			}}})
		})
		issuer = httptest.NewServer(mux)

		// What the reconciler does for an ExternalIdentityProvider
		Expect(auth.PersistExternalIdentityProvider(ctx, &v1.ExternalIdentityProvider{
			ObjectMeta: metav1.ObjectMeta{Name: "appx", Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.ExternalIdentityProviderSpec{
				Issuer:   issuer.URL,
				Audience: audience,
				Claims:   v1.ExternalIdentityClaims{Name: "name", Email: "email"},
			},
		})).To(Succeed())

		allowed = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("fed-allowed"), Type: lo.ToPtr("Kubernetes::Pod"), ConfigClass: "Pod", Tags: types.JSONStringMap{"tenant": "a"}}
		denied = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("fed-denied"), Type: lo.ToPtr("Kubernetes::Pod"), ConfigClass: "Pod", Tags: types.JSONStringMap{"tenant": "b"}}
		Expect(ctx.DB().Create(&allowed).Error).To(Succeed())
		Expect(ctx.DB().Create(&denied).Error).To(Succeed())

		for _, p := range []struct {
			name, category string
			into           *models.Playbook
		}{
			{"fed-e2e-echo", "Kubernetes", &echoPlaybook},
			{"fed-e2e-other", "Other", &otherPlaybook},
		} {
			playbookCRD := &v1.Playbook{
				ObjectMeta: metav1.ObjectMeta{Name: p.name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec: v1.PlaybookSpec{
					Category: p.category,
					Configs:  []types.ResourceSelector{{Types: []string{"Kubernetes::Pod"}}},
					Actions:  []v1.PlaybookAction{{Name: "echo", Exec: &v1.ExecAction{Script: `echo "hello from tina"`}}},
				},
			}
			Expect(db.PersistPlaybookFromCRD(ctx, playbookCRD)).To(Succeed())
			Expect(ctx.DB().Where("id = ?", string(playbookCRD.UID)).First(p.into).Error).To(Succeed())
		}

		for _, scope := range []*v1.Scope{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "kubernetes-playbooks", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Playbook: &types.ResourceSelector{FieldSelector: "category=Kubernetes"}}}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "all-configs", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{Name: "*"}}}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &types.ResourceSelector{TagSelector: "tenant=a"}}}},
			},
		} {
			Expect(db.PersistScopeFromCRD(ctx, scope)).To(Succeed())
		}

		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "playbook-runner", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{
					Name:        "read-configs",
					Description: "Read configs",
					Action:      policy.ActionRead,
					Resource:    v1.ScopeReference{ScopeRef: "all-configs"},
				},
				{
					Name:        "run-kubernetes",
					Description: "Run Kubernetes playbooks on configs",
					Action:      policy.ActionPlaybookRun,
					Resource:    v1.ScopeReference{ScopeRef: "kubernetes-playbooks"},
					Target:      &v1.ScopeReference{ScopeRef: "all-configs"},
				},
			}},
		}
		Expect(db.PersistRoleFromCRD(ctx, role)).To(Succeed())

		Expect(db.PersistRoleBindingFromCRD(ctx, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-operators", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleBindingSpec{
				Role: "playbook-runner",
				Subjects: v1.RoleBindingSubjects{OIDC: []v1.OIDCSubject{{
					Provider: "appx",
					Match:    "'operators' in claims.groups && claims.tenant == 'a'",
				}}},
				Constraints: []v1.RoleBindingConstraint{
					{Rule: "read-configs", Resource: &v1.ScopeReference{ScopeRef: "tenant-a"}},
					{Rule: "run-kubernetes", Target: &v1.ScopeReference{ScopeRef: "tenant-a"}},
				},
			},
		})).To(Succeed())

		// What the notify listeners do when roles and role_bindings change
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		Expect(auth.RebuildOIDCBindings(ctx)).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		issuer.Close()
	})

	ginkgo.It("authenticates Tina as a federated person", func() {
		code, body := request(http.MethodGet, "/auth/whoami", operator(), nil)
		Expect(code).To(Equal(http.StatusOK), string(body))

		var whoami struct {
			Payload struct {
				User  models.Person `json:"user"`
				Roles []string      `json:"roles"`
			} `json:"payload"`
		}
		Expect(json.Unmarshal(body, &whoami)).To(Succeed())
		Expect(whoami.Payload.User.Name).To(Equal("Tina"))
		Expect(whoami.Payload.User.Type).To(Equal(db.PersonTypeFederated))
		Expect(whoami.Payload.User.ExternalID).To(Equal("appx:tina-123"))
		Expect(lo.Uniq(whoami.Payload.Roles)).To(ConsistOf(models.BindingPrincipal(namespace, "tenant-a-operators")))
	})

	ginkgo.It("runs a playbook on a config Tina may read, recorded as Tina", func() {
		code, body := request(http.MethodPost, "/playbook/run", operator(), map[string]any{
			"id":        echoPlaybook.ID,
			"config_id": allowed.ID,
		})
		Expect(code).To(Equal(http.StatusCreated), string(body))

		var resp struct {
			RunID string `json:"run_id"`
		}
		Expect(json.Unmarshal(body, &resp)).To(Succeed())

		var run models.PlaybookRun
		Eventually(func() models.PlaybookRunStatus {
			Expect(DefaultContext.DB().Where("id = ?", resp.RunID).First(&run).Error).To(Succeed())
			return run.Status
		}, 30*time.Second, 500*time.Millisecond).Should(Equal(models.PlaybookRunStatusCompleted))

		var tina models.Person
		Expect(DefaultContext.DB().Where("external_id = ?", "appx:tina-123").First(&tina).Error).To(Succeed())
		Expect(run.CreatedBy).ToNot(BeNil())
		Expect(*run.CreatedBy).To(Equal(tina.ID))

		var action models.PlaybookRunAction
		Expect(DefaultContext.DB().Where("playbook_run_id = ?", run.ID).First(&action).Error).To(Succeed())
		Expect(action.Result["stdout"]).To(ContainSubstring("hello from tina"))
	})

	ginkgo.It("refuses to run a playbook outside the rule's resource, even on a tenant a config", func() {
		code, body := request(http.MethodPost, "/playbook/run", operator(), map[string]any{
			"id":        otherPlaybook.ID,
			"config_id": allowed.ID,
		})
		Expect(code).To(Equal(http.StatusForbidden), string(body))
	})

	ginkgo.It("refuses to run the playbook on a config outside the constraint's target", func() {
		code, body := request(http.MethodPost, "/playbook/run", operator(), map[string]any{
			"id":        echoPlaybook.ID,
			"config_id": denied.ID,
		})
		Expect(code).To(Equal(http.StatusForbidden), string(body))
	})

	ginkgo.It("refuses to run the playbook once Tina's claims no longer match the binding", func() {
		code, body := request(http.MethodPost, "/playbook/run", tinaToken("b", "operators"), map[string]any{
			"id":        echoPlaybook.ID,
			"config_id": allowed.ID,
		})
		Expect(code).To(Equal(http.StatusForbidden), string(body))
	})

	ginkgo.It("filters /db reads to the rows Tina's bindings cover", func() {
		code, body := request(http.MethodGet, "/db/config_items?select=id", "admin", nil)
		Expect(code).To(Equal(http.StatusOK), string(body))
		Expect(configIDs(body)).To(ContainElements(allowed.ID.String(), denied.ID.String()))

		code, body = request(http.MethodGet, "/db/config_items?select=id", operator(), nil)
		Expect(code).To(Equal(http.StatusOK), string(body))
		Expect(configIDs(body)).To(ContainElement(allowed.ID.String()))
		Expect(configIDs(body)).ToNot(ContainElement(denied.ID.String()))

		code, body = request(http.MethodGet, "/db/people?select=id", operator(), nil)
		Expect(code).To(Equal(http.StatusForbidden), string(body))
	})

	ginkgo.It("doesn't let Tina create an access token", func() {
		code, body := request(http.MethodPost, "/auth/create_token", operator(), map[string]any{"name": "escape"})
		Expect(code).To(Equal(http.StatusForbidden), string(body))
	})

	ginkgo.It("rejects a token signed with another key", func() {
		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).ToNot(HaveOccurred())
		now := time.Now()
		forged := sign(otherKey, jwt.MapClaims{
			"iss": issuer.URL, "aud": audience, "sub": "tina-123", "tenant": "a", "groups": []string{"operators"},
			"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
		})
		code, body := request(http.MethodGet, "/auth/whoami", forged, nil)
		Expect(code).To(Equal(http.StatusUnauthorized), string(body))
	})

	ginkgo.It("leaves tokens from unregistered issuers to the other authenticators", func() {
		now := time.Now()
		stranger := sign(signingKey, jwt.MapClaims{
			"iss": "https://stranger.test", "aud": audience, "sub": "tina-123",
			"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
		})
		code, body := request(http.MethodGet, "/auth/whoami", stranger, nil)
		Expect(code).To(Equal(http.StatusUnauthorized), string(body))
	})

	ginkgo.It("still lets the admin in with basic auth", func() {
		code, body := request(http.MethodGet, "/auth/whoami", "admin", nil)
		Expect(code).To(Equal(http.StatusOK), string(body))
	})
})
