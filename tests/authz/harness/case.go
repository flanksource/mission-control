package harness

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Case is one thing a person may only do with a grant, checked through every route that reaches it.
//
// Each case gets a person of its own and runs these steps in order, each against the live server:
//
//  1. Without credentials, the request is refused as unauthenticated.
//  2. Without grants, it's denied.
//  3. With each near miss alone, it's denied.
//  4. With the grant, it's allowed.
//  5. With an access token bound to a copy of the grant's Role, it's allowed.
//  6. With an access token bound to a near miss, it's denied, though the token's owner is allowed.
//  7. Once the token's Role is deleted, that token is denied, and another token and its owner are still allowed.
//  8. Once the grant's RoleBinding is deleted, the person and their remaining token are denied.
//  9. Once the remaining token is deleted, it's refused as unauthenticated.
//
// A case that's Never allowed runs steps 1 and 2, and checks that it's still denied with the grant and through a
// token bound to it.
type Case struct {
	Name string

	// Arrange sets up what one request needs, as the admin, and returns the action's requests. It's called before
	// each request.
	Arrange func(f *Fixture) Attempt

	// Grant are the rules that let a person make the request.
	Grant []Rule

	// NearMisses are grants close to Grant that must not let a person make the request.
	NearMisses []NearMiss

	// Never is set when no grant may let a person make the request. Grant is then what's checked not to.
	Never bool
}

// NearMiss is a grant that must not let a person make a case's request.
type NearMiss struct {
	Name  string
	Rules []Rule
}

// Attempt is what a case's action needs once: the requests that each take the action through one route, and how
// to tell what one did. Arrange is called again before each request, so a request always finds what it needs, e.g. a
// connection to delete.
type Attempt struct {
	// Requests take the action, each through another route, e.g. /connection and /db. Every step checks each.
	Requests []Request

	// Happened reports whether a request did what it asks, e.g. that a connection is gone, checked as the admin
	// after the request is answered. A denied request must not have happened, and an allowed one must have.
	Happened func() bool

	// Verify checks an allowed response further, e.g. that it doesn't reveal credentials.
	Verify func(Response)

	// Denied are the statuses a denied request is answered with. By default, 403 Forbidden.
	Denied []int
}

type outcome string

const (
	allowed         outcome = "allowed"
	denied          outcome = "denied"
	unauthenticated outcome = "unauthenticated"
)

var env struct {
	server  *Server
	barrier *barrier
	cases   int
}

// UsersNeeded is how many users StartServer has to create for the registered cases.
func UsersNeeded() int {
	// One more for the barrier
	return env.cases + 1
}

// Use runs the cases against the server.
func Use(server *Server) {
	ginkgo.GinkgoHelper()
	env.server = server
	env.barrier = newBarrier(server.NewUser())
}

// Describe registers the cases, as a container of the text.
func Describe(text string, cases ...Case) bool {
	return ginkgo.Describe(text, func() {
		for _, c := range cases {
			env.cases++
			describeCase(env.cases, c)
		}
	})
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// namespaceFor is the namespace the case's Scopes, Roles, RoleBindings and fixtures are created in.
func namespaceFor(n int, name string) string {
	slug := strings.Trim(nonAlphanumeric.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return fmt.Sprintf("authz-%02d-%s", n, slug)
}

type run struct {
	c        Case
	user     *Caller
	fixture  *Fixture
	grant    grant
	extra    []grant
	tokens   map[string]*Caller
	tokenIDs map[string]string
}

func describeCase(n int, c Case) {
	r := &run{c: c, tokens: map[string]*Caller{}, tokenIDs: map[string]string{}}

	ginkgo.Describe(c.Name, ginkgo.Ordered, func() {
		ginkgo.BeforeAll(func() {
			r.user = env.server.NewUser()
			r.fixture = newFixture(namespaceFor(n, c.Name))
		})

		ginkgo.AfterAll(func() {
			for name, id := range r.tokenIDs {
				r.user.Do(DELETE("/auth/token/" + id))
				delete(r.tokenIDs, name)
			}
			for _, g := range append(r.extra, r.grant) {
				for _, o := range append(append(g.bindings, g.role), g.scopes...) {
					if o.name != "" {
						env.server.Admin.Do(DELETE(o.path()))
					}
				}
			}
			r.fixture.cleanup()
		})

		ginkgo.It("refuses a request without credentials", func() {
			r.expect(r.user.Anonymous(), unauthenticated)
		})

		ginkgo.It("denies a person without grants", func() {
			r.expect(r.user, denied)
		})

		if c.Never {
			r.describeNever()
			return
		}

		for i, miss := range c.NearMisses {
			ginkgo.It("denies a person granted only "+miss.Name, func() {
				g := writeRole(r.fixture.Namespace, fmt.Sprintf("near-miss-%d", i), miss.Rules)
				g.bindPerson(r.user.Email())
				r.extra = append(r.extra, g)
				env.barrier.wait()

				r.expect(r.user, denied)

				r.extra[len(r.extra)-1].delete()
				r.extra = r.extra[:len(r.extra)-1]
			})
		}

		ginkgo.It("allows a person granted it", func() {
			r.grant = writeRole(r.fixture.Namespace, "grant", c.Grant)
			r.grant.bindPerson(r.user.Email())
			env.barrier.wait()

			r.expect(r.user, allowed)
		})

		ginkgo.It("allows an access token bound to the grant's Role", func() {
			for _, name := range []string{"token-a", "token-b"} {
				r.extra = append(r.extra, writeRole(r.fixture.Namespace, name, c.Grant))
			}
			for i, miss := range c.NearMisses {
				r.extra = append(r.extra, writeRole(r.fixture.Namespace, fmt.Sprintf("token-near-miss-%d", i), miss.Rules))
			}
			env.barrier.wait()

			for _, name := range []string{"token-a", "token-b"} {
				r.createToken(name, name)
				r.expect(r.tokens[name], allowed)
			}
		})

		for i, miss := range c.NearMisses {
			ginkgo.It("denies an access token bound to "+miss.Name+", though its owner is allowed", func() {
				name := fmt.Sprintf("token-near-miss-%d", i)
				r.createToken(name, name)
				r.expect(r.tokens[name], denied)
				r.expect(r.user, allowed)
			})
		}

		ginkgo.It("denies an access token once its Role is deleted, and no other token or its owner", func() {
			i := slices.IndexFunc(r.extra, func(g grant) bool { return g.role.name == "token-a" })
			r.extra[i].delete()
			r.extra = slices.Delete(r.extra, i, i+1)
			env.barrier.wait()

			r.expect(r.tokens["token-a"], denied)
			r.expect(r.tokens["token-b"], allowed)
			r.expect(r.user, allowed)
		})

		ginkgo.It("denies the person and their tokens once the grant's RoleBinding is deleted", func() {
			r.grant.unbind()
			env.barrier.wait()

			r.expect(r.user, denied)
			r.expect(r.tokens["token-b"], denied)
		})

		ginkgo.It("refuses a deleted access token", func() {
			resp := r.user.Do(DELETE("/auth/token/" + r.tokenIDs["token-b"]))
			Expect(resp.Status).To(BeElementOf(http.StatusOK, http.StatusNoContent), resp.String())
			delete(r.tokenIDs, "token-b")

			r.expect(r.tokens["token-b"], unauthenticated)
		})
	})
}

func (r *run) describeNever() {
	ginkgo.It("denies a person granted every action it could take", func() {
		r.grant = writeRole(r.fixture.Namespace, "grant", r.c.Grant)
		r.grant.bindPerson(r.user.Email())
		r.extra = append(r.extra, writeRole(r.fixture.Namespace, "token-a", r.c.Grant))
		env.barrier.wait()

		r.expect(r.user, denied)
	})

	ginkgo.It("denies an access token bound to the same grant", func() {
		r.createToken("token-a", "token-a")
		r.expect(r.tokens["token-a"], denied)
	})
}

// createToken creates an access token of the case's person, through their session, bound to the case's Role.
func (r *run) createToken(name, role string) {
	ginkgo.GinkgoHelper()
	resp := r.user.Do(POST("/auth/create_token", map[string]any{
		"name":  name,
		"roles": []any{map[string]any{"namespace": r.fixture.Namespace, "name": role}},
	}))
	Expect(resp.Status).To(Equal(http.StatusOK), resp.String())

	var created struct {
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}
	Expect(resp.Decode(&created)).To(Succeed())
	Expect(created.Payload.Token).ToNot(BeEmpty(), resp.String())
	r.tokens[name] = r.user.WithToken(name, created.Payload.Token)

	list := r.user.Do(GET("/auth/tokens"))
	Expect(list.Status).To(Equal(http.StatusOK), list.String())
	var tokens struct {
		Payload []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"payload"`
	}
	Expect(list.Decode(&tokens)).To(Succeed())
	for _, t := range tokens.Payload {
		if t.Name == name {
			r.tokenIDs[name] = t.ID
		}
	}
	Expect(r.tokenIDs).To(HaveKey(name), "token %s isn't listed: %s", name, list)
}

// expect takes the case's action as the caller through each of its routes, once each, and checks each was answered
// as want, and did or didn't happen accordingly.
func (r *run) expect(caller *Caller, want outcome) {
	ginkgo.GinkgoHelper()
	routes := len(r.c.Arrange(r.fixture).Requests)
	Expect(routes).To(BeNumerically(">", 0), "the case has no requests")
	for i := range routes {
		r.expectRoute(caller, want, i)
	}
}

func (r *run) expectRoute(caller *Caller, want outcome, route int) {
	ginkgo.GinkgoHelper()
	attempt := r.c.Arrange(r.fixture)
	request := attempt.Requests[route]
	resp := caller.Do(request)
	Expect(resp.Err).ToNot(HaveOccurred())

	deniedStatuses := attempt.Denied
	if len(deniedStatuses) == 0 {
		deniedStatuses = []int{http.StatusForbidden}
	}

	var got outcome
	switch {
	case resp.Status == http.StatusUnauthorized:
		got = unauthenticated
	case slices.Contains(deniedStatuses, resp.Status):
		got = denied
	case resp.Status >= 200 && resp.Status < 300:
		got = allowed
	default:
		ginkgo.Fail(fmt.Sprintf("%s: expected it %s, but it was answered with neither an allow nor a denial: %s", caller, want, resp))
	}
	Expect(got).To(Equal(want), "%s: %s", caller, resp)

	if attempt.Happened != nil {
		if want == allowed {
			Expect(attempt.Happened()).To(BeTrue(), "%s was allowed for %s, but didn't happen", request, caller)
		} else {
			Expect(attempt.Happened()).To(BeFalse(), "%s was refused for %s, but happened anyway", request, caller)
		}
	}
	if want == allowed && attempt.Verify != nil {
		attempt.Verify(resp)
	}
}
