package permissions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/agent"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
)

var _ = Describe("Agent access tokens", Ordered, func() {
	const namespace = "agent-tokens"

	type registered struct {
		agent models.Agent
		token *models.AccessToken
	}

	var (
		e                      *echo.Echo
		reader, writer, nobody *models.Person
		first, second          registered
		personal               *models.AccessToken
		writerToken            *models.AccessToken
	)

	register := func(name string) registered {
		GinkgoHelper()
		person, err := db.CreatePerson(DefaultContext, name, name+"@local", db.PersonTypeAgent)
		Expect(err).ToNot(HaveOccurred())
		Expect(db.CreateAgent(DefaultContext, name, &person.ID, nil)).To(Succeed())
		found, err := db.FindAgentByPersonID(DefaultContext, person.ID)
		Expect(err).ToNot(HaveOccurred())
		_, token, err := db.CreateAccessToken(DefaultContext, person.ID, "default", lo.ToPtr(time.Hour), nil, false, nil)
		Expect(err).ToNot(HaveOccurred())
		return registered{agent: *found, token: token}
	}

	call := func(ctx context.Context, method, path string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, path, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	as := func(person *models.Person) context.Context { return DefaultContext.WithUser(person) }

	tokensPath := func(a registered) string { return "/agent/" + a.agent.ID.String() + "/tokens" }

	exists := func(token *models.AccessToken) bool {
		GinkgoHelper()
		var count int64
		Expect(DefaultContext.DB().Model(&models.AccessToken{}).Where("id = ?", token.ID).Count(&count).Error).To(Succeed())
		return count == 1
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())
		suffix := uuid.NewString()[:8]

		reader = setup.CreateUserWithRole(DefaultContext, "Agent Token Reader", "agent-token-reader-"+suffix+"@test.com")
		writer = setup.CreateUserWithRole(DefaultContext, "Agent Token Writer", "agent-token-writer-"+suffix+"@test.com")
		nobody = setup.CreateUserWithRole(DefaultContext, "Agent Token Nobody", "agent-token-nobody-"+suffix+"@test.com")
		bindWholeType(namespace, "read-agents", v1.ResourceAgent, boundPeople(reader), policy.ActionRead)
		bindWholeType(namespace, "update-agents", v1.ResourceAgent, boundPeople(writer), policy.ActionRead, policy.ActionUpdate)
		Expect(rbac.ReloadPolicy()).To(Succeed())

		first = register("first-agent-" + suffix)
		second = register("second-agent-" + suffix)

		var err error
		_, personal, err = db.CreateAccessTokenForPerson(DefaultContext, writer, "personal", time.Hour, false,
			models.AccessTokenRoles{{Namespace: namespace, Name: "update-agents"}})
		Expect(err).ToNot(HaveOccurred())
		writerToken = personal

		e = echo.New()
		agent.RegisterRoutes(e)
	})

	AfterAll(func() {
		for _, a := range []registered{first, second} {
			DefaultContext.DB().Where("person_id = ?", a.agent.PersonID).Delete(&models.AccessToken{})
			DefaultContext.DB().Delete(&a.agent)
		}
		DefaultContext.DB().Where("id = ?", personal.ID).Delete(&models.AccessToken{})
		deleteNamespace(namespace)
	})

	It("lists an agent's own tokens, without their values, to a subject that may read agents", func() {
		rec := call(as(reader), http.MethodGet, tokensPath(first))
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var body struct {
			Payload []map[string]any `json:"payload"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(lo.Map(body.Payload, func(t map[string]any, _ int) any { return t["id"] })).To(Equal([]any{first.token.ID.String()}))
		Expect(body.Payload[0]).To(HaveKey("expires_at"))
		Expect(body.Payload[0]).ToNot(HaveKey("value"))

		Expect(call(as(nobody), http.MethodGet, tokensPath(first)).Code).To(Equal(http.StatusForbidden))
		Expect(call(as(reader), http.MethodGet, "/agent/"+uuid.NewString()+"/tokens").Code).To(Equal(http.StatusNotFound))
	})

	It("refuses to delete an agent's token to a subject that may only read agents", func() {
		Expect(call(as(reader), http.MethodDelete, tokensPath(first)+"/"+first.token.ID.String()).Code).To(Equal(http.StatusForbidden))
		Expect(exists(first.token)).To(BeTrue())
	})

	It("deletes only a token of the agent in the path", func() {
		Expect(call(as(writer), http.MethodDelete, tokensPath(second)+"/"+first.token.ID.String()).Code).To(Equal(http.StatusNotFound))
		Expect(call(as(writer), http.MethodDelete, tokensPath(first)+"/"+personal.ID.String()).Code).To(Equal(http.StatusNotFound))
		Expect(exists(first.token)).To(BeTrue())
		Expect(exists(personal)).To(BeTrue())
	})

	It("refuses to issue or delete an agent's token with an access token", func() {
		tokenCtx := mcRBAC.WithAccessToken(as(writer), writerToken)

		rec := call(tokenCtx, http.MethodDelete, tokensPath(first)+"/"+first.token.ID.String())
		Expect(rec.Code).To(Equal(http.StatusForbidden))
		Expect(rec.Body.String()).To(ContainSubstring("with an access token"))
		Expect(exists(first.token)).To(BeTrue())

		rec = call(tokenCtx, http.MethodPost, "/agent/token")
		Expect(rec.Code).To(Equal(http.StatusForbidden))
		Expect(rec.Body.String()).To(ContainSubstring("with an access token"))
	})

	It("deletes an agent's token to a subject that may update agents", func() {
		Expect(call(as(writer), http.MethodDelete, tokensPath(first)+"/"+first.token.ID.String()).Code).To(Equal(http.StatusOK))
		Expect(exists(first.token)).To(BeFalse())
		Expect(exists(second.token)).To(BeTrue())
	})
})
