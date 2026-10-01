package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("External identity providers", ginkgo.Ordered, func() {
	const (
		audience  = "https://mission-control.test"
		namespace = "federation-test"
		keyID     = "appx-key"
	)

	var (
		signingKey *rsa.PrivateKey
		issuer     *httptest.Server
		provider   *v1.ExternalIdentityProvider
	)

	sign := func(key *rsa.PrivateKey, claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = keyID
		signed, err := token.SignedString(key)
		Expect(err).ToNot(HaveOccurred())
		return signed
	}

	tokenClaims := func(overrides map[string]any) jwt.MapClaims {
		now := time.Now()
		claims := jwt.MapClaims{
			"iss":    issuer.URL,
			"aud":    audience,
			"sub":    "tina-123",
			"name":   "Tina",
			"email":  "tina@client.com",
			"groups": []string{"operators", "viewers"},
			"iat":    now.Unix(),
			"exp":    now.Add(time.Minute).Unix(),
		}
		for k, v := range overrides {
			if v == nil {
				delete(claims, k)
			} else {
				claims[k] = v
			}
		}
		return claims
	}

	// serve runs a request with the token through federatedSession and returns the context the handler saw
	serve := func(token string) (bool, *httptest.ResponseRecorder, *context.Context) {
		req := httptest.NewRequest(http.MethodGet, "/playbook/list", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
		req = req.WithContext(DefaultContext)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)

		var served *context.Context
		handled, err := federatedSession(c, func(c echo.Context) error {
			ctx := c.Request().Context().(context.Context)
			served = &ctx
			return c.NoContent(http.StatusOK)
		})
		Expect(err).ToNot(HaveOccurred())
		return handled, rec, served
	}

	serveOK := func(claims jwt.MapClaims) context.Context {
		handled, rec, ctx := serve(sign(signingKey, claims))
		Expect(handled).To(BeTrue())
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(ctx).ToNot(BeNil())
		return *ctx
	}

	ginkgo.BeforeAll(func() {
		Expect(dutyRBAC.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

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

		provider = &v1.ExternalIdentityProvider{
			ObjectMeta: metav1.ObjectMeta{Name: "appx", Namespace: "default", UID: k8sTypes.UID(uuid.NewString()), CreationTimestamp: metav1.Now()},
			Spec: v1.ExternalIdentityProviderSpec{
				Issuer:   issuer.URL,
				Audience: audience,
				Claims:   v1.ExternalIdentityClaims{Name: "name", Email: "email"},
			},
		}
		Expect(PersistExternalIdentityProvider(DefaultContext, provider)).To(Succeed())

		scope := &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "all-playbooks", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Playbook: &types.ResourceSelector{Name: "*"}}}},
		}
		Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())

		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "run-playbooks", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{{
				Name:     "run",
				Action:   policy.ActionPlaybookRun,
				Resource: v1.ScopeReference{ScopeRef: "all-playbooks"},
			}}},
		}
		Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())

		for name, match := range map[string]string{
			"appx-operators": "'operators' in claims.groups",
			"appx-tenant-a":  "claims.tenant == 'a'",
		} {
			binding := &v1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec: v1.RoleBindingSpec{
					Role:     "run-playbooks",
					Subjects: v1.RoleBindingSubjects{OIDC: []v1.OIDCSubject{{Provider: "appx", Match: match}}},
				},
			}
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
		}

		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		Expect(RebuildOIDCBindings(DefaultContext)).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		Expect(DeleteExternalIdentityProvider(DefaultContext, string(provider.UID))).To(Succeed())
		issuer.Close()

		var people []models.Person
		Expect(DefaultContext.DB().Where("external_id LIKE ?", "appx:%").Find(&people).Error).To(Succeed())
		for _, person := range people {
			_, err := dutyRBAC.Enforcer().DeleteRolesForUser(models.FederatedPrincipal(person.ID.String()))
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(DefaultContext.DB().Exec("DELETE FROM people WHERE external_id LIKE ?", "appx:%").Error).To(Succeed())

		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		Expect(RebuildOIDCBindings(DefaultContext)).To(Succeed())
	})

	ginkgo.It("represents the user as a federated person", func() {
		ctx := serveOK(tokenClaims(nil))

		person := ctx.User()
		Expect(person).ToNot(BeNil())
		Expect(person.Type).To(Equal(db.PersonTypeFederated))
		Expect(person.ExternalID).To(Equal("appx:tina-123"))
		Expect(person.Name).To(Equal("Tina"))
		Expect(person.Email).To(BeEmpty())
		Expect(person.Properties).To(Equal(models.PersonProperties{Provider: "appx", Email: "tina@client.com"}))

		Expect(ctx.Subject()).To(Equal(models.FederatedPrincipal(person.ID.String())))
	})

	bindingsOf := func(ctx context.Context) []string {
		bindings, err := dutyRBAC.Enforcer().GetRolesForUser(ctx.Subject())
		Expect(err).ToNot(HaveOccurred())
		return bindings
	}

	canRunPlaybooks := func(ctx context.Context) bool {
		attr := &models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "any-playbook"}}
		return dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionPlaybookRun)
	}

	ginkgo.It("makes the user a member of the role bindings whose match accepts the token's claims", func() {
		ctx := serveOK(tokenClaims(map[string]any{"tenant": "a"}))
		Expect(bindingsOf(ctx)).To(ConsistOf(
			models.BindingPrincipal(namespace, "appx-operators"),
			models.BindingPrincipal(namespace, "appx-tenant-a"),
		))
	})

	ginkgo.It("treats a match that fails to evaluate, e.g. on a missing claim, as no match", func() {
		ctx := serveOK(tokenClaims(nil))
		Expect(bindingsOf(ctx)).To(ConsistOf(models.BindingPrincipal(namespace, "appx-operators")))
	})

	ginkgo.It("authorizes only through role bindings", func() {
		ctx := serveOK(tokenClaims(nil))

		Expect(canRunPlaybooks(ctx)).To(BeTrue())

		// no implicit viewer access and no everyone role
		Expect(dutyRBAC.CheckContext(ctx, policy.ObjectCatalog, policy.ActionRead)).To(BeFalse())
		hasEveryone, err := dutyRBAC.Enforcer().HasRoleForUser(ctx.Subject(), policy.RoleEveryone)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasEveryone).To(BeFalse())

		for _, binding := range bindingsOf(ctx) {
			hasEveryone, err := dutyRBAC.Enforcer().HasRoleForUser(binding, policy.RoleEveryone)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasEveryone).To(BeFalse())
		}
	})

	ginkgo.It("reuses the same person across requests", func() {
		first := serveOK(tokenClaims(nil))
		second := serveOK(tokenClaims(nil))
		Expect(second.User().ID).To(Equal(first.User().ID))

		var count int64
		Expect(DefaultContext.DB().Model(&models.Person{}).Where("external_id = ?", "appx:tina-123").Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(1)))
	})

	ginkgo.It("revokes the bindings the token's claims no longer match on the next request", func() {
		Expect(canRunPlaybooks(serveOK(tokenClaims(nil)))).To(BeTrue())

		ctx := serveOK(tokenClaims(map[string]any{"groups": []string{"viewers"}}))
		Expect(bindingsOf(ctx)).To(BeEmpty())
		Expect(canRunPlaybooks(ctx)).To(BeFalse())
	})

	ginkgo.It("rejects a role binding with an invalid match", func() {
		for _, match := range []string{"claims.groups +", "'operators'", "unknown == 'a'"} {
			binding := &v1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-match", Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
				Spec: v1.RoleBindingSpec{
					Role:     "run-playbooks",
					Subjects: v1.RoleBindingSubjects{OIDC: []v1.OIDCSubject{{Provider: "appx", Match: match}}},
				},
			}
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).ToNot(Succeed(), match)
		}
	})

	ginkgo.It("does not let federated users create access tokens", func() {
		req := httptest.NewRequest(http.MethodPost, "/auth/create_token", strings.NewReader(`{"name":"escape"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+sign(signingKey, tokenClaims(nil)))
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req.WithContext(DefaultContext), rec)

		handled, err := federatedSession(c, CreateToken)
		Expect(err).ToNot(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
	})

	ginkgo.It("leaves tokens from other issuers to other authenticators", func() {
		handled, _, _ := serve(sign(signingKey, tokenClaims(map[string]any{"iss": "https://someone-else.test"})))
		Expect(handled).To(BeFalse())

		handled, _, _ = serve("not-a-jwt")
		Expect(handled).To(BeFalse())
	})

	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rejected := map[string]func() string{
		"an expired token": func() string {
			return sign(signingKey, tokenClaims(map[string]any{
				"iat": time.Now().Add(-3 * time.Minute).Unix(),
				"exp": time.Now().Add(-2 * time.Minute).Unix(),
			}))
		},
		"a token for another audience": func() string {
			return sign(signingKey, tokenClaims(map[string]any{"aud": "https://other.test"}))
		},
		"a token living longer than maxTokenAge": func() string {
			return sign(signingKey, tokenClaims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}))
		},
		"a token without exp": func() string {
			return sign(signingKey, tokenClaims(map[string]any{"exp": nil}))
		},
		"a token issued in the future": func() string {
			return sign(signingKey, tokenClaims(map[string]any{"iat": time.Now().Add(time.Hour).Unix(), "exp": time.Now().Add(61 * time.Minute).Unix()}))
		},
		"a token without a username": func() string {
			return sign(signingKey, tokenClaims(map[string]any{"sub": nil}))
		},
		"a token signed with another key": func() string {
			return sign(otherKey, tokenClaims(nil))
		},
		"a token signed with a shared secret": func() string {
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims(nil)).SignedString([]byte("secret"))
			Expect(err).ToNot(HaveOccurred())
			return signed
		},
	}

	for name, token := range rejected {
		ginkgo.It("rejects "+name, func() {
			handled, rec, _ := serve(token())
			Expect(handled).To(BeTrue())
			Expect(rec.Code).To(Equal(http.StatusUnauthorized))
		})
	}

	ginkgo.It("rejects every token when the provider is disabled", func() {
		disabled := provider.DeepCopy()
		disabled.Spec.Disabled = true
		Expect(PersistExternalIdentityProvider(DefaultContext, disabled)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(PersistExternalIdentityProvider(DefaultContext, provider)).To(Succeed())
		})

		handled, rec, _ := serve(sign(signingKey, tokenClaims(nil)))
		Expect(handled).To(BeTrue())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
		Expect(rec.Header().Get("WWW-Authenticate")).ToNot(BeEmpty())
	})

	newerDuplicate := func(mutate func(*v1.ExternalIdentityProvider)) *v1.ExternalIdentityProvider {
		duplicate := provider.DeepCopy()
		duplicate.UID = k8sTypes.UID(uuid.NewString())
		duplicate.CreationTimestamp = metav1.NewTime(provider.CreationTimestamp.Add(time.Minute))
		mutate(duplicate)
		ginkgo.DeferCleanup(func() {
			Expect(DeleteExternalIdentityProvider(DefaultContext, string(duplicate.UID))).To(Succeed())
		})
		return duplicate
	}

	ginkgo.It("rejects a newer provider for the same issuer", func() {
		duplicate := newerDuplicate(func(p *v1.ExternalIdentityProvider) { p.Name = "appy" })
		Expect(PersistExternalIdentityProvider(DefaultContext, duplicate)).ToNot(Succeed())

		handled, rec, _ := serve(sign(signingKey, tokenClaims(nil)))
		Expect(handled).To(BeTrue())
		Expect(rec.Code).To(Equal(http.StatusOK), "the older provider keeps verifying the issuer's tokens")
	})

	ginkgo.It("rejects a newer provider with the same name in another namespace", func() {
		duplicate := newerDuplicate(func(p *v1.ExternalIdentityProvider) {
			p.Namespace = "a-namespace-that-sorts-first"
			p.Spec.Issuer = "https://other.appx.example.com"
		})
		Expect(PersistExternalIdentityProvider(DefaultContext, duplicate)).ToNot(Succeed())
	})

	ginkgo.It("lets the oldest provider win, whichever is reconciled first", func() {
		older := newerDuplicate(func(p *v1.ExternalIdentityProvider) {
			p.Name = "appy"
			p.CreationTimestamp = metav1.NewTime(provider.CreationTimestamp.Add(-time.Minute))
		})

		var changed []string
		ProviderValidityChanged = func(namespace, name string) { changed = append(changed, namespace+"/"+name) }
		ginkgo.DeferCleanup(func() {
			ProviderValidityChanged = nil
			Expect(DeleteExternalIdentityProvider(DefaultContext, string(older.UID))).To(Succeed())
			Expect(PersistExternalIdentityProvider(DefaultContext, provider)).To(Succeed())
		})

		Expect(PersistExternalIdentityProvider(DefaultContext, older)).To(Succeed())
		Expect(changed).To(ConsistOf(provider.Namespace + "/" + provider.Name))
		Expect(PersistExternalIdentityProvider(DefaultContext, provider)).ToNot(Succeed())

		changed = nil
		Expect(DeleteExternalIdentityProvider(DefaultContext, string(older.UID))).To(Succeed())
		Expect(changed).To(ConsistOf(provider.Namespace+"/"+provider.Name), "the next oldest takes effect without being re-applied")
		Expect(PersistExternalIdentityProvider(DefaultContext, provider)).To(Succeed())
	})

	ginkgo.It("rejects insecure issuers", func() {
		insecure := provider.Spec
		insecure.Issuer = "http://appx.example.com"
		Expect(insecure.Validate()).ToNot(Succeed())
	})
})

var _ = ginkgo.Describe("Signing key redirects", func() {
	requests := func(urls ...string) []*http.Request {
		var reqs []*http.Request
		for _, u := range urls {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			Expect(err).ToNot(HaveOccurred())
			reqs = append(reqs, req)
		}
		return reqs
	}

	check := func(to string, via ...string) error {
		return keyFetchClient.CheckRedirect(requests(to)[0], requests(via...))
	}

	ginkgo.It("follows five redirects, and refuses a sixth", func() {
		hops := []string{"https://a.example.com/0"}
		for i := 1; i <= maxKeyRedirects; i++ {
			Expect(check("https://a.example.com/next", hops...)).To(Succeed(), "redirect %d", i)
			hops = append(hops, "https://a.example.com/next")
		}
		Expect(check("https://a.example.com/next", hops...)).ToNot(Succeed())
	})

	ginkgo.It("refuses a redirect from https to http, even between loopback hosts", func() {
		Expect(check("http://auth.example.com/keys", "https://auth.example.com/keys")).ToNot(Succeed())
		Expect(check("http://localhost/keys", "https://localhost/keys")).ToNot(Succeed())
		Expect(check("http://localhost/keys", "http://localhost/start", "https://localhost/keys")).ToNot(Succeed())
	})

	ginkgo.It("follows plain http only to a loopback host", func() {
		Expect(check("http://127.0.0.1/keys", "http://localhost/keys")).To(Succeed())
		Expect(check("http://auth.example.com/keys", "http://localhost/keys")).ToNot(Succeed())
	})
})
