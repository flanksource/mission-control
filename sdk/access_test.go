package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	groupA  = "11111111-1111-1111-1111-111111111111"
	groupB  = "22222222-2222-2222-2222-222222222222"
	userOne = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userTwo = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	config1 = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	roleOne = "dddddddd-dddd-dddd-dddd-dddddddddddd"
)

// pgRoutes serves a PostgREST-shaped GET per table, recording the query each
// table was asked for so the tests can assert on the exact filters.
func pgRoutes(bodies map[string]string, seen map[string]url.Values) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Expect(r.Method).To(Equal(http.MethodGet))
		table := strings.TrimPrefix(r.URL.Path, "/db/")
		body, ok := bodies[table]
		if !ok {
			ginkgo.Fail("unexpected request: " + r.URL.Path)
		}
		if seen != nil {
			seen[table] = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

var _ = ginkgo.Describe("access grants", func() {
	ginkgo.It("filters config_access_summary and resolves group names", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"config_access_summary": fmt.Sprintf(`[
				{"config_id":%q,"config_name":"prod-app","config_type":"Azure::EnterpriseApplication","external_user_id":%q,"user":"jane","email":"jane@example.com","role":"Owner","user_type":"Human"},
				{"config_id":%q,"config_name":"prod-app","config_type":"Azure::EnterpriseApplication","external_user_id":%q,"user":"sre-bot","email":"","role":"Reader","user_type":"group","external_group_id":%q}
			]`, config1, userOne, config1, userTwo, groupA),
			"external_groups": fmt.Sprintf(`[{"id":%q,"name":"sre-team"}]`, groupA),
		}, seen)
		defer server.Close()

		grants, total, err := New(server.URL, "tok").ListAccessGrants(context.Background(), AccessGrantOptions{
			ConfigIDs:  []string{config1},
			ConfigType: "Azure::EnterpriseApplication",
			Role:       "Owner",
			UserType:   "Human",
			User:       "jane",
			Limit:      10,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(-1), "no Content-Range header means unknown total")
		Expect(grants).To(HaveLen(2))

		q := seen["config_access_summary"]
		Expect(q.Get("deleted_at")).To(Equal("is.null"))
		Expect(q.Get("order")).To(Equal("config_name,config_id,user,external_user_id,external_group_id,role,created_at"))
		Expect(q.Get("config_id")).To(Equal("in.(" + config1 + ")"))
		Expect(q.Get("config_type.filter")).To(Equal("Azure::EnterpriseApplication"))
		Expect(q.Get("role.filter")).To(Equal("Owner"))
		Expect(q.Get("user_type.filter")).To(Equal("Human"))
		Expect(q.Get("or")).To(Equal("(user.ilike.*jane*,email.ilike.*jane*)"))
		Expect(q.Get("limit")).To(Equal("10"))

		Expect(seen["external_groups"].Get("id")).To(Equal("in.(" + groupA + ")"))
		Expect(grants[0].GroupName).To(BeEmpty())
		Expect(grants[0].RoleSource()).To(Equal("direct"))
		Expect(grants[1].GroupName).To(Equal("sre-team"))
		Expect(grants[1].RoleSource()).To(Equal("group:sre-team"))
	})

	ginkgo.It("reports the exact total from Content-Range so callers can detect truncation", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Header.Get("Prefer")).To(Equal("count=exact"))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Range", "0-0/3573")
			_, _ = fmt.Fprintf(w, `[{"config_id":%q,"config_name":"a","external_user_id":%q,"user":"jane","role":"Owner"}]`, config1, userOne)
		}))
		defer server.Close()

		grants, total, err := New(server.URL, "tok").ListAccessGrants(context.Background(), AccessGrantOptions{Limit: 1})
		Expect(err).ToNot(HaveOccurred())
		Expect(grants).To(HaveLen(1))
		Expect(total).To(Equal(3573))
	})

	ginkgo.It("does not request an exact count for unlimited queries", func() {
		var preferSeen atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Prefer") != "" {
				preferSeen.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		_, _, err := New(server.URL, "tok").ListAccessGrants(context.Background(), AccessGrantOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(preferSeen.Load()).To(BeFalse())
	})

	ginkgo.It("resolves /db under the /api prefix when the context server is a frontend URL", func() {
		var gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		_, _, err := New(server.URL+"/api", "tok").ListAccessGrants(context.Background(), AccessGrantOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(gotPath).To(Equal("/api/db/config_access_summary"))
	})
})

var _ = ginkgo.Describe("ExpandGroupAccess", func() {
	signedIn := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	added := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	revoked := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	reviewed := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)

	groupAID := uuid.MustParse(groupA)
	groupBID := uuid.MustParse(groupB)

	groupGrant := AccessGrant{
		ConfigAccessSummary: models.ConfigAccessSummary{
			ConfigID:        uuid.MustParse(config1),
			ConfigName:      "prod-app",
			ExternalGroupID: &groupAID,
			User:            "sre-team",
			Role:            "Reader",
			UserType:        "group",
			LastReviewedAt:  &reviewed,
		},
		GroupName: "sre-team",
	}
	directGrant := AccessGrant{
		ConfigAccessSummary: models.ConfigAccessSummary{
			ConfigID:       uuid.MustParse(config1),
			ConfigName:     "prod-app",
			ExternalUserID: uuid.MustParse(userOne),
			User:           "jane",
			Role:           "Owner",
			UserType:       "Human",
		},
	}

	members := []GroupMember{
		{
			GroupID: groupAID, GroupName: "sre-team",
			UserID: uuid.MustParse(userTwo), UserName: "bob", Email: "bob@example.com", UserType: "Human",
			LastSignedInAt: &signedIn, MembershipAddedAt: added,
		},
		{
			GroupID: groupAID, GroupName: "sre-team",
			UserID: uuid.MustParse(userOne), UserName: "jane", Email: "jane@example.com", UserType: "Human",
			MembershipAddedAt: added, MembershipDeletedAt: &revoked,
		},
		{
			GroupID: groupBID, GroupName: "other-team",
			UserID: uuid.MustParse(userOne), UserName: "jane", MembershipAddedAt: added,
		},
	}

	ginkgo.It("emits one synthetic grant per active member of the granting group", func() {
		out := ExpandGroupAccess([]AccessGrant{directGrant, groupGrant}, members)

		Expect(out).To(HaveLen(3), "direct grant, group grant, and one active member")
		Expect(out[0].User).To(Equal("jane"))
		Expect(out[1].User).To(Equal("sre-team"))

		expanded := out[2]
		Expect(expanded.User).To(Equal("bob"))
		Expect(expanded.Email).To(Equal("bob@example.com"))
		Expect(expanded.ExternalUserID).To(Equal(uuid.MustParse(userTwo)))
		Expect(expanded.Role).To(Equal("Reader"))
		Expect(expanded.GroupName).To(Equal("sre-team"), "the synthetic row keeps its group provenance")
		Expect(expanded.RoleSource()).To(Equal("group:sre-team"))
		Expect(expanded.LastSignedInAt).To(Equal(&signedIn))
		Expect(expanded.CreatedAt).To(Equal(added))
		Expect(expanded.LastReviewedAt).To(BeNil(), "the member inherits no review of their own")
	})

	ginkgo.It("does not expand members of a group that holds no grant", func() {
		out := ExpandGroupAccess([]AccessGrant{directGrant}, members)
		Expect(out).To(HaveLen(1))
	})

	ginkgo.It("keys on group id so same-named groups do not cross-contaminate", func() {
		collidingGrant := groupGrant
		collidingGrant.ExternalGroupID = &groupBID

		out := ExpandGroupAccess([]AccessGrant{collidingGrant}, members)
		Expect(out).To(HaveLen(2))
		Expect(out[1].User).To(Equal("jane"), "only group B's member is expanded")
	})
})

var _ = ginkgo.Describe("access rollups", func() {
	ginkgo.It("delegates to the server view only when no filter is unanswerable by it", func() {
		Expect(AccessGrantOptions{}.CanUseUserRollup()).To(BeTrue())
		Expect(AccessGrantOptions{}.CanUseConfigRollup()).To(BeTrue())

		byType := AccessGrantOptions{ConfigType: "Azure::EnterpriseApplication"}
		Expect(byType.CanUseConfigRollup()).To(BeTrue(), "the config view keeps its config columns")
		Expect(byType.CanUseUserRollup()).To(BeFalse(), "the user view aggregates config_type away")

		byUser := AccessGrantOptions{User: "jane"}
		Expect(byUser.CanUseConfigRollup()).To(BeFalse())
		Expect(byUser.CanUseUserRollup()).To(BeFalse())

		Expect(AccessGrantOptions{Role: "Owner"}.CanUseConfigRollup()).To(BeFalse())
		Expect(AccessGrantOptions{UserType: "Human"}.CanUseConfigRollup()).To(BeFalse())
		Expect(AccessGrantOptions{GroupIDs: []string{groupA}}.CanUseConfigRollup()).To(BeFalse())
	})

	ginkgo.It("pushes config filters into the by-config view", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"config_access_summary_by_config": fmt.Sprintf(`[{"config_id":%q,"config_name":"prod-app","config_type":"Azure::EnterpriseApplication","access_count":12,"distinct_users":9,"distinct_roles":3}]`, config1),
		}, seen)
		defer server.Close()

		rows, _, err := New(server.URL, "tok").ListAccessSummaryByConfig(context.Background(), AccessGrantOptions{
			ConfigIDs:  []string{config1},
			ConfigType: "Azure::EnterpriseApplication",
			Limit:      25,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].AccessCount).To(Equal(12))
		Expect(rows[0].DistinctUsers).To(Equal(9))

		q := seen["config_access_summary_by_config"]
		Expect(q.Get("order")).To(Equal("access_count.desc,config_name,config_id"))
		Expect(q.Get("config_id")).To(Equal("in.(" + config1 + ")"))
		Expect(q.Get("config_type.filter")).To(Equal("Azure::EnterpriseApplication"))
		Expect(q.Get("limit")).To(Equal("25"))
	})

	ginkgo.It("orders the by-user view by grant count", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"config_access_summary_by_user": fmt.Sprintf(`[{"external_user_id":%q,"user":"jane","email":"jane@example.com","access_count":31,"distinct_roles":4,"distinct_configs":7}]`, userOne),
		}, seen)
		defer server.Close()

		rows, _, err := New(server.URL, "tok").ListAccessSummaryByUser(context.Background(), 5)

		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].DistinctConfigs).To(Equal(7))
		Expect(seen["config_access_summary_by_user"].Get("order")).To(Equal("access_count.desc,user"))
		Expect(seen["config_access_summary_by_user"].Get("limit")).To(Equal("5"))
	})
})

var _ = ginkgo.Describe("external identities", func() {
	ginkgo.It("matches users on name, email or alias and excludes soft-deleted rows", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"external_users": fmt.Sprintf(`[{"id":%q,"name":"jane","email":"jane@example.com","user_type":"Human"}]`, userOne),
		}, seen)
		defer server.Close()

		users, _, err := New(server.URL, "tok").ListExternalUsers(context.Background(), IdentityOptions{
			Name: "jane", Type: "Human", Limit: 20,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(users).To(HaveLen(1))

		q := seen["external_users"]
		Expect(q.Get("deleted_at")).To(Equal("is.null"))
		Expect(q.Get("order")).To(Equal("name"))
		Expect(q.Get("user_type.filter")).To(Equal("Human"))
		Expect(q.Get("or")).To(BeEmpty())
		Expect(q.Get("limit")).To(Equal("20"))
	})

	ginkgo.It("reads groups from the summary view so counts arrive rolled up", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"external_group_summary": fmt.Sprintf(`[{"id":%q,"name":"sre-team","group_type":"SecurityGroup","members_count":8,"permissions_count":34}]`, groupA),
		}, seen)
		defer server.Close()

		groups, _, err := New(server.URL, "tok").ListExternalGroups(context.Background(), IdentityOptions{Type: "SecurityGroup"})

		Expect(err).ToNot(HaveOccurred())
		Expect(groups).To(HaveLen(1))
		Expect(groups[0].Name).To(Equal("sre-team"))
		Expect(groups[0].MembersCount).To(Equal(8))
		Expect(groups[0].PermissionsCount).To(Equal(34))
		Expect(seen["external_group_summary"].Get("group_type.filter")).To(Equal("SecurityGroup"))
	})

	ginkgo.It("resolves a UUID argument to an exact id match without an ilike scan", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"external_users": fmt.Sprintf(`[{"id":%q,"name":"jane"}]`, userOne),
		}, seen)
		defer server.Close()

		user, err := New(server.URL, "tok").ResolveExternalUser(context.Background(), userOne)

		Expect(err).ToNot(HaveOccurred())
		Expect(user.Name).To(Equal("jane"))
		Expect(seen["external_users"].Get("id")).To(Equal("eq." + userOne))
		Expect(seen["external_users"].Get("or")).To(BeEmpty())
		Expect(seen["external_users"].Get("limit")).To(BeEmpty())
	})

	ginkgo.It("errors on an ambiguous name rather than picking one", func() {
		server := pgRoutes(map[string]string{
			"external_users": fmt.Sprintf(`[{"id":%q,"name":"jane doe"},{"id":%q,"name":"jane roe"}]`, userOne, userTwo),
		}, nil)
		defer server.Close()

		_, err := New(server.URL, "tok").ResolveExternalUser(context.Background(), "jane")
		Expect(err).To(MatchError(ContainSubstring(`"jane" matches multiple users: jane doe, jane roe`)))
	})

	ginkgo.It("errors when nothing matches", func() {
		server := pgRoutes(map[string]string{"external_groups": `[]`}, nil)
		defer server.Close()

		_, err := New(server.URL, "tok").ResolveExternalGroup(context.Background(), "nope")
		Expect(err).To(MatchError(ContainSubstring(`no external group matches "nope"`)))
	})

	ginkgo.It("hydrates group membership with identity, sign-in and revocation state", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"external_user_groups": fmt.Sprintf(`[
				{"external_user_id":%q,"external_group_id":%q,"created_at":"2026-01-02T00:00:00Z"},
				{"external_user_id":%q,"external_group_id":%q,"created_at":"2026-01-02T00:00:00Z","deleted_at":"2026-02-03T00:00:00Z"}
			]`, userTwo, groupA, userOne, groupA),
			"external_users": fmt.Sprintf(`[
				{"id":%q,"name":"bob","email":"bob@example.com","user_type":"Human"},
				{"id":%q,"name":"aaron","email":"aaron@example.com","user_type":"Human"}
			]`, userTwo, userOne),
			"config_access_summary_by_user": fmt.Sprintf(`[{"external_user_id":%q,"last_signed_in_at":"2026-03-01T00:00:00Z"}]`, userTwo),
			"external_groups":               fmt.Sprintf(`[{"id":%q,"name":"sre-team","group_type":"SecurityGroup"}]`, groupA),
		}, seen)
		defer server.Close()

		members, err := New(server.URL, "tok").GetGroupMembers(context.Background(), []string{groupA})

		Expect(err).ToNot(HaveOccurred())
		Expect(members).To(HaveLen(2))

		Expect(members[0].UserName).To(Equal("bob"), "active memberships sort ahead of revoked ones")
		Expect(members[0].Active()).To(BeTrue())
		Expect(members[0].GroupName).To(Equal("sre-team"))
		Expect(members[0].Email).To(Equal("bob@example.com"))
		Expect(members[0].LastSignedInAt).ToNot(BeNil())

		Expect(members[1].UserName).To(Equal("aaron"))
		Expect(members[1].Active()).To(BeFalse())
		Expect(members[1].LastSignedInAt).To(BeNil())

		Expect(seen["external_user_groups"].Get("external_group_id")).To(Equal("in.(" + groupA + ")"))
	})

	ginkgo.It("splits role holders into users and groups", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"config_access": fmt.Sprintf(`[
				{"external_user_id":%q},
				{"external_group_id":%q}
			]`, userOne, groupA),
			"external_users":  fmt.Sprintf(`[{"id":%q,"name":"jane"}]`, userOne),
			"external_groups": fmt.Sprintf(`[{"id":%q,"name":"sre-team"}]`, groupA),
		}, seen)
		defer server.Close()

		holders, err := New(server.URL, "tok").GetRoleHolders(context.Background(), roleOne)

		Expect(err).ToNot(HaveOccurred())
		Expect(holders.Users).To(HaveLen(1))
		Expect(holders.Users[0].Name).To(Equal("jane"))
		Expect(holders.Groups).To(HaveLen(1))
		Expect(holders.Groups[0].Name).To(Equal("sre-team"))
		Expect(seen["config_access"].Get("external_role_id")).To(Equal("eq." + roleOne))
		Expect(seen["config_access"].Get("deleted_at")).To(Equal("is.null"))
	})

	ginkgo.It("batches large id lists so the request URL cannot overflow", func() {
		ids := make([]string, 250)
		for i := range ids {
			ids[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
		}

		var batches atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			batches.Add(1)
			filter := r.URL.Query().Get("id")
			Expect(strings.Count(filter, ",")).To(BeNumerically("<", accessIDBatchSize))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		refs, err := New(server.URL, "tok").ConfigRefs(context.Background(), ids)
		Expect(err).ToNot(HaveOccurred())
		Expect(refs).To(BeEmpty())
		Expect(batches.Load()).To(Equal(int32(3)), "250 ids at a batch size of 100")
	})
})

var _ = ginkgo.Describe("parseContentRangeTotal", func() {
	for _, tt := range []struct {
		header   string
		expected int
	}{
		{"0-24/3573", 3573},
		{"*/0", 0},
		{"0-24/*", -1},
		{"", -1},
		{"nonsense", -1},
	} {
		ginkgo.It(fmt.Sprintf("parses %q as %d", tt.header, tt.expected), func() {
			Expect(parseContentRangeTotal(tt.header)).To(Equal(tt.expected))
		})
	}
})

var _ = ginkgo.Describe("inList", func() {
	ginkgo.It("drops blanks and duplicates and sorts so the same set yields the same URL", func() {
		Expect(inList([]string{"b", "a", "", "b"})).To(Equal("in.(a,b)"))
	})
})
