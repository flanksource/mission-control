package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/patrickmn/go-cache"

	"github.com/flanksource/incident-commander/db"
)

type agentAuthFixture struct {
	agent *models.Agent
	token string
}

func createAgentWithToken(ctx context.Context, name string) agentAuthFixture {
	username := "agent-" + uuid.NewString()[:8]
	person, err := db.CreatePerson(ctx, username, username+"@local", db.PersonTypeAgent)
	Expect(err).ToNot(HaveOccurred())

	token, _, err := db.CreateAccessToken(ctx, person.ID, "default", nil, nil, false, nil)
	Expect(err).ToNot(HaveOccurred())

	Expect(db.CreateAgent(ctx, name, &person.ID, nil)).To(Succeed())
	agent, err := db.FindAgentByPersonID(ctx, person.ID)
	Expect(err).ToNot(HaveOccurred())
	Expect(agent).ToNot(BeNil())

	return agentAuthFixture{agent: agent, token: token.PlainText()}
}

func createPersonWithToken(ctx context.Context) string {
	username := "person-" + uuid.NewString()[:8]
	person, err := db.CreatePerson(ctx, username, username+"@example.com", "")
	Expect(err).ToNot(HaveOccurred())

	token, _, err := db.CreateAccessTokenForPerson(ctx, person, "default", 0, false, viewerRole)
	Expect(err).ToNot(HaveOccurred())
	return token.PlainText()
}

// requestAgent sends a request through the middleware and returns the agent on the handler's context.
func requestAgent(middleware echo.MiddlewareFunc, setAuth func(*http.Request), agentName string) (*models.Agent, int) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/upstream/push?agent_name="+agentName, nil)
	req = req.WithContext(DefaultContext)
	setAuth(req)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var agent *models.Agent
	handler := middleware(func(c echo.Context) error {
		agent = c.Request().Context().(context.Context).Agent()
		return c.NoContent(http.StatusOK)
	})
	Expect(handler(c)).To(Succeed())
	return agent, rec.Code
}

var _ = ginkgo.Describe("Agent resolution from access token", ginkgo.Ordered, func() {
	var (
		agent        agentAuthFixture
		otherAgent   agentAuthFixture
		deletedAgent agentAuthFixture
		personToken  string
	)

	ginkgo.BeforeAll(func() {
		suffix := uuid.NewString()[:8]
		agent = createAgentWithToken(DefaultContext, "token-agent-"+suffix)
		otherAgent = createAgentWithToken(DefaultContext, "other-agent-"+suffix)
		deletedAgent = createAgentWithToken(DefaultContext, "deleted-agent-"+suffix)
		// Deleting an agent revokes its tokens, but a token already in the cache still authenticates.
		_, err := getAccessToken(DefaultContext, deletedAgent.token)
		Expect(err).ToNot(HaveOccurred())
		Expect(DefaultContext.DB().Model(&models.Agent{}).Where("id = ?", deletedAgent.agent.ID).
			Update("deleted_at", duty.Now()).Error).To(Succeed())
		personToken = createPersonWithToken(DefaultContext)
	})

	kratos := &kratosMiddleware{
		tokenCache:         cache.New(time.Hour, time.Hour),
		accessTokenCache:   cache.New(time.Hour, time.Hour),
		authSessionCache:   cache.New(time.Hour, time.Hour),
		basicAuthSeparator: "separator",
	}
	clerk := ClerkHandler{
		tokenCache:       cache.New(time.Hour, time.Hour),
		accessTokenCache: cache.New(time.Hour, time.Hour),
		userCache:        cache.New(time.Hour, time.Hour),
	}

	basicToken := func(token string) func(*http.Request) {
		return func(r *http.Request) { r.SetBasicAuth("token", token) }
	}
	bearerToken := func(token string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set(echo.HeaderAuthorization, "Bearer "+token) }
	}

	modes := []struct {
		name       string
		middleware echo.MiddlewareFunc
		auth       func(string) func(*http.Request)
	}{
		{name: "kratos basic auth", middleware: kratos.Session, auth: basicToken},
		{name: "kratos bearer", middleware: kratos.Session, auth: bearerToken},
		{name: "clerk basic auth", middleware: clerk.Session, auth: basicToken},
		{name: "clerk bearer", middleware: clerk.Session, auth: bearerToken},
		{name: "basic bearer", middleware: basicAuthMiddleware, auth: bearerToken},
	}

	for _, mode := range modes {
		ginkgo.Context(mode.name, func() {
			ginkgo.BeforeEach(func() {
				localhostOnly = false
			})

			ginkgo.It("acts as the token's agent whatever agent_name says", func() {
				for _, name := range []string{agent.agent.Name, otherAgent.agent.Name, "unknown-agent", ""} {
					got, code := requestAgent(mode.middleware, mode.auth(agent.token), name)
					Expect(code).To(Equal(http.StatusOK), fmt.Sprintf("agent_name=%q", name))
					Expect(got).ToNot(BeNil(), fmt.Sprintf("agent_name=%q", name))
					Expect(got.ID).To(Equal(agent.agent.ID), fmt.Sprintf("agent_name=%q", name))
				}
			})

			ginkgo.It("sets no agent for a person's token", func() {
				got, code := requestAgent(mode.middleware, mode.auth(personToken), agent.agent.Name)
				Expect(code).To(Equal(http.StatusOK))
				Expect(got).To(BeNil())
			})

			ginkgo.It("sets no agent for a deleted agent's token", func() {
				got, code := requestAgent(mode.middleware, mode.auth(deletedAgent.token), deletedAgent.agent.Name)
				Expect(code).To(Equal(http.StatusOK))
				Expect(got).To(BeNil())
			})
		})
	}
})
