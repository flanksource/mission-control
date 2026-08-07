package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/formatters"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testUserID  = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	testUser2ID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	testGroupID = "11111111-1111-1111-1111-111111111111"
	testRoleID  = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	testConfig1 = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	testConfig2 = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
)

// accessServer serves a canned PostgREST body per table, recording the query
// each table was asked for. An unexpected table fails the spec rather than
// returning an empty result that would silently pass.
func accessServer(bodies map[string]string, seen map[string]url.Values) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

var _ = ginkgo.Describe("faro access listings", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("pushes the user filters down to external_users", func() {
		seen := map[string]url.Values{}
		server := accessServer(map[string]string{
			"external_users": fmt.Sprintf(`[{"id":%q,"name":"jane","email":"jane@example.com","user_type":"Human"}]`, testUserID),
		}, seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		users, err := listAccessUsers(accessUserListOpts{Name: "jane", Type: "Human", Limit: 25})

		Expect(err).ToNot(HaveOccurred())
		Expect(users).To(HaveLen(1))
		Expect(users[0].Name).To(Equal("jane"))
		Expect(seen["external_users"].Get("user_type.filter")).To(Equal("Human"))
		Expect(seen["external_users"].Get("limit")).To(Equal("25"))
		Expect(seen["external_users"].Get("deleted_at")).To(Equal("is.null"))
		Expect(seen["external_users"].Get("or")).To(BeEmpty())
	})

	ginkgo.It("reads groups from the summary view so counts come with them", func() {
		seen := map[string]url.Values{}
		server := accessServer(map[string]string{
			"external_group_summary": fmt.Sprintf(`[{"id":%q,"name":"sre-team","group_type":"SecurityGroup","members_count":8,"permissions_count":34}]`, testGroupID),
		}, seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		groups, err := listAccessGroups(accessGroupListOpts{Type: "SecurityGroup", Limit: 25})

		Expect(err).ToNot(HaveOccurred())
		Expect(groups).To(HaveLen(1))
		Expect(groups[0].MembersCount).To(Equal(8))
		Expect(groups[0].PermissionsCount).To(Equal(34))
		Expect(seen["external_group_summary"].Get("group_type.filter")).To(Equal("SecurityGroup"))
	})

	ginkgo.It("wraps roles so clicky can key them by id and name", func() {
		server := accessServer(map[string]string{
			"external_roles": fmt.Sprintf(`[{"id":%q,"name":"Owner","role_type":"BuiltInRole"}]`, testRoleID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		roles, err := listAccessRoles(accessRoleListOpts{Limit: 25})

		Expect(err).ToNot(HaveOccurred())
		Expect(roles).To(HaveLen(1))
		Expect(roles[0].GetID()).To(Equal(testRoleID))
		Expect(roles[0].GetName()).To(Equal("Owner"))
	})
})

var _ = ginkgo.Describe("faro access detail views", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("assembles a user from their grants and group membership", func() {
		server := accessServer(map[string]string{
			"external_users":        fmt.Sprintf(`[{"id":%q,"name":"jane","email":"jane@example.com","user_type":"Human"}]`, testUserID),
			"config_access_summary": fmt.Sprintf(`[{"config_id":%q,"config_name":"prod-app","config_type":"Azure::EnterpriseApplication","external_user_id":%q,"user":"jane","role":"Owner"}]`, testConfig1, testUserID),
			"external_user_groups":  fmt.Sprintf(`[{"external_group_id":%q}]`, testGroupID),
			"external_groups":       fmt.Sprintf(`[{"id":%q,"name":"sre-team","group_type":"SecurityGroup"}]`, testGroupID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		out, err := getAccessUser(testUserID, map[string]string{})

		Expect(err).ToNot(HaveOccurred())
		result, ok := out.(*AccessUserGetResult)
		Expect(ok).To(BeTrue())
		Expect(result.User.Name).To(Equal("jane"))
		Expect(result.Access).To(HaveLen(1))
		Expect(result.Groups).To(HaveLen(1))

		pretty := result.Pretty().String()
		Expect(pretty).To(ContainSubstring("Groups (1)"))
		Expect(pretty).To(ContainSubstring("Access (1)"))
	})

	ginkgo.It("skips the sections whose get flag is off", func() {
		server := accessServer(map[string]string{
			"external_users": fmt.Sprintf(`[{"id":%q,"name":"jane"}]`, testUserID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		out, err := getAccessUser(testUserID, map[string]string{"access": "false", "groups": "false"})

		Expect(err).ToNot(HaveOccurred())
		Expect(out.(*AccessUserGetResult).Access).To(BeEmpty())
		Expect(out.(*AccessUserGetResult).Groups).To(BeEmpty())
	})

	ginkgo.It("assembles a group from its membership and grants", func() {
		server := accessServer(map[string]string{
			"external_groups":               fmt.Sprintf(`[{"id":%q,"name":"sre-team","group_type":"SecurityGroup"}]`, testGroupID),
			"external_user_groups":          fmt.Sprintf(`[{"external_user_id":%q,"external_group_id":%q,"created_at":"2026-01-02T00:00:00Z"}]`, testUserID, testGroupID),
			"external_users":                fmt.Sprintf(`[{"id":%q,"name":"jane","email":"jane@example.com","user_type":"Human"}]`, testUserID),
			"config_access_summary_by_user": `[]`,
			"config_access_summary":         fmt.Sprintf(`[{"config_id":%q,"config_name":"prod-app","external_group_id":%q,"user":"sre-team","role":"Reader"}]`, testConfig1, testGroupID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		out, err := getAccessGroup(testGroupID, map[string]string{})

		Expect(err).ToNot(HaveOccurred())
		result := out.(*AccessGroupGetResult)
		Expect(result.Group.Name).To(Equal("sre-team"))
		Expect(result.Members).To(HaveLen(1))
		Expect(result.Members[0].UserName).To(Equal("jane"))
		Expect(result.Members[0].Email).To(Equal("jane@example.com"))
		Expect(result.Access).To(HaveLen(1))
		Expect(result.Access[0].RoleSource()).To(Equal("group:sre-team"))
	})

	ginkgo.It("splits role holders into users and groups", func() {
		server := accessServer(map[string]string{
			"external_roles":  fmt.Sprintf(`[{"id":%q,"name":"Owner","role_type":"BuiltInRole"}]`, testRoleID),
			"config_access":   fmt.Sprintf(`[{"external_user_id":%q},{"external_group_id":%q}]`, testUserID, testGroupID),
			"external_users":  fmt.Sprintf(`[{"id":%q,"name":"jane"}]`, testUserID),
			"external_groups": fmt.Sprintf(`[{"id":%q,"name":"sre-team"}]`, testGroupID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		out, err := getAccessRole(testRoleID, map[string]string{})

		Expect(err).ToNot(HaveOccurred())
		result := out.(*AccessRoleGetResult)
		Expect(result.Role.Name).To(Equal("Owner"))
		Expect(result.Users).To(HaveLen(1))
		Expect(result.Groups).To(HaveLen(1))
	})

	ginkgo.It("fails loudly when no Mission Control context is configured", func() {
		Expect(clientcmd.SaveConfig(&clientcmd.MCConfig{})).To(Succeed())

		_, _, err := accessClient()

		Expect(err).To(MatchError(ContainSubstring("no Mission Control server context configured")))
	})
})

var _ = ginkgo.Describe("resolveConfigIDs", func() {
	ginkgo.It("returns no filter for an empty query without calling the server", func() {
		ids, err := resolveConfigIDs(context.Background(), sdk.New("http://127.0.0.1:1", "tok"), nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(ids).To(BeNil())
	})

	ginkgo.It("joins the positional args into one catalog search", func() {
		var body bytes.Buffer
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/resources/search"))
			_, _ = body.ReadFrom(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"configs":[{"id":%q},{"id":%q}]}`, testConfig1, testConfig2)
		}))
		defer server.Close()

		ids, err := resolveConfigIDs(context.Background(), sdk.New(server.URL, "tok"), []string{"type=Kubernetes::Pod", "name=api*"})

		Expect(err).ToNot(HaveOccurred())
		Expect(ids).To(ConsistOf(testConfig1, testConfig2))
		Expect(body.String()).To(ContainSubstring(`type=Kubernetes::Pod name=api*`))
	})

	ginkgo.It("errors instead of exporting everything when the query matches nothing", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"configs":[]}`))
		}))
		defer server.Close()

		_, err := resolveConfigIDs(context.Background(), sdk.New(server.URL, "tok"), []string{"name=nope"})

		Expect(err).To(MatchError(ContainSubstring(`no configs match "name=nope"`)))
	})
})

var _ = ginkgo.Describe("parseSince", func() {
	ginkgo.It("returns no lower bound for an empty value", func() {
		since, err := parseSince("")

		Expect(err).ToNot(HaveOccurred())
		Expect(since).To(BeNil())
	})

	for _, tt := range []struct {
		value string
		ago   time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
	} {
		ginkgo.It(fmt.Sprintf("resolves %q to %s ago", tt.value, tt.ago), func() {
			since, err := parseSince(tt.value)

			Expect(err).ToNot(HaveOccurred())
			Expect(*since).To(BeTemporally("~", time.Now().Add(-tt.ago), time.Minute))
		})
	}

	ginkgo.It("rejects an unparseable duration", func() {
		_, err := parseSince("last tuesday")

		Expect(err).To(MatchError(ContainSubstring(`invalid --since "last tuesday"`)))
	})
})

// grant builds one crosstab row for the rollup and rendering specs.
func grant(configID, configName, userID, user, role string, created time.Time) sdk.AccessGrant {
	return sdk.AccessGrant{ConfigAccessSummary: models.ConfigAccessSummary{
		ConfigID:       uuid.MustParse(configID),
		ConfigName:     configName,
		ConfigType:     "Azure::EnterpriseApplication",
		ExternalUserID: uuid.MustParse(userID),
		User:           user,
		Role:           role,
		UserType:       "Human",
		CreatedAt:      created,
	}}
}

var _ = ginkgo.Describe("access rollups", func() {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// jane holds Owner on prod-app twice (two scraped grants) plus Reader on
	// staging-app; bob holds Reader on prod-app only.
	grants := []sdk.AccessGrant{
		grant(testConfig1, "prod-app", testUserID, "jane", "Owner", older),
		grant(testConfig2, "staging-app", testUserID, "jane", "Reader", newer),
		grant(testConfig1, "prod-app", testUserID, "jane", "Owner", older),
		grant(testConfig1, "prod-app", testUser2ID, "bob", "Reader", newer),
	}

	ginkgo.It("counts distinct configs and roles per user, busiest first", func() {
		rows := rollupByUser(grants)

		Expect(rows).To(HaveLen(2))
		Expect(rows[0].User).To(Equal("jane"))
		Expect(rows[0].AccessCount).To(Equal(3))
		Expect(rows[0].DistinctConfigs).To(Equal(2))
		Expect(rows[0].DistinctRoles).To(Equal(2))
		Expect(*rows[0].LatestGrant).To(Equal(newer))
		Expect(rows[1].User).To(Equal("bob"))
		Expect(rows[1].AccessCount).To(Equal(1))
	})

	ginkgo.It("counts distinct users and roles per config, busiest first", func() {
		rows := rollupByConfig(grants)

		Expect(rows).To(HaveLen(2))
		Expect(rows[0].ConfigName).To(Equal("prod-app"))
		Expect(rows[0].AccessCount).To(Equal(3))
		Expect(rows[0].DistinctUsers).To(Equal(2))
		Expect(rows[0].DistinctRoles).To(Equal(2))
		Expect(*rows[0].LatestGrant).To(Equal(newer))
		Expect(rows[1].ConfigName).To(Equal("staging-app"))
	})

	ginkgo.It("returns no rows for no grants", func() {
		Expect(rollupByUser(nil)).To(BeEmpty())
		Expect(rollupByConfig(nil)).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("later", func() {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	ginkgo.It("keeps the more recent of two timestamps", func() {
		Expect(*later(&early, &late)).To(Equal(late))
		Expect(*later(&late, &early)).To(Equal(late))
	})

	ginkgo.It("ignores nil and zero candidates", func() {
		Expect(*later(&early, nil)).To(Equal(early))
		Expect(*later(&early, &time.Time{})).To(Equal(early))
		Expect(later(nil, nil)).To(BeNil())
	})

	ginkgo.It("copies the winner so a rollup never aliases the caller's field", func() {
		candidate := late

		result := later(nil, &candidate)
		candidate = early

		Expect(*result).To(Equal(late))
	})
})

var _ = ginkgo.Describe("access permissions output", func() {
	grants := []sdk.AccessGrant{
		grant(testConfig2, "staging-app", testUserID, "jane", "Reader", time.Time{}),
		grant(testConfig1, "prod-app", testUserID, "jane", "Owner", time.Time{}),
		grant(testConfig1, "prod-app", testUser2ID, "bob", "Reader", time.Time{}),
	}

	ginkgo.It("groups pretty output by config, alphabetically", func() {
		out := AccessPermissionsResult{Rows: grants}.Pretty().String()

		Expect(out).To(ContainSubstring("Access matrix: 3 entries across 2 configs"))
		Expect(out).To(ContainSubstring("prod-app (Azure::EnterpriseApplication)"))
		Expect(out).To(ContainSubstring("staging-app (Azure::EnterpriseApplication)"))
		Expect(strings.Index(out, "prod-app")).To(BeNumerically("<", strings.Index(out, "staging-app")))
	})

	ginkgo.It("marks expanded output so synthesised rows are not read as raw grants", func() {
		Expect(AccessPermissionsResult{Rows: grants, Expanded: true}.Pretty().String()).To(ContainSubstring("(expanded)"))
	})

	ginkgo.It("says so when nothing matched", func() {
		Expect(AccessPermissionsResult{}.Pretty().String()).To(ContainSubstring("No access entries found."))
	})

	ginkgo.It("exports one flat CSV row per grant, each attributable to its config", func() {
		row := grants[1]
		row.Email = "jane@example.com"
		row.GroupName = "sre-team"

		out, err := formatters.NewCSVFormatter().Format(principalGrantRows([]sdk.AccessGrant{row}), formatters.FormatOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(strings.TrimSpace(out), "\n")).To(Equal([]string{
			"Config,Type,User,Email,Role,Source,User Type,Last Signed In",
			"prod-app,Azure::EnterpriseApplication,jane,jane@example.com,Owner,group:sre-team,Human,-",
		}))
	})
})

// The identity models embed types.NoOpResourceSelectable, which clicky reflects
// into a column of its own despite the field's `json:"-"` tag. Left to reflection
// every listing exports that internal alongside scraper ids and raw timestamps,
// so each row type declares its columns; these specs pin them.
var _ = ginkgo.Describe("faro access listing columns", func() {
	email := "jane@example.com"
	created := time.Now().Add(-2 * time.Hour)

	ginkgo.It("exports users without the reflected NoOpResourceSelectable column", func() {
		row := externalUserRow{models.ExternalUser{
			Name: "jane", Email: &email, UserType: "Human", Tenant: "acme", CreatedAt: created,
		}}

		out, err := formatters.NewCSVFormatter().Format([]externalUserRow{row}, formatters.FormatOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(strings.TrimSpace(out), "\n")).To(Equal([]string{
			"Name,Email,Type,Tenant,Created",
			"jane,jane@example.com,Human,acme,2.0h",
		}))
	})

	ginkgo.It("exports groups with their member and permission counts", func() {
		row := externalGroupSummaryRow{sdk.ExternalGroupSummary{
			ExternalGroup:    models.ExternalGroup{Name: "sre-team", GroupType: "SecurityGroup", Tenant: "acme", CreatedAt: created},
			MembersCount:     4,
			PermissionsCount: 12,
		}}

		out, err := formatters.NewCSVFormatter().Format([]externalGroupSummaryRow{row}, formatters.FormatOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(strings.TrimSpace(out), "\n")).To(Equal([]string{
			"Name,Type,Tenant,Members,Permissions,Created",
			"sre-team,SecurityGroup,acme,4,12,2.0h",
		}))
	})

	ginkgo.It("exports roles as flat rows rather than a nested struct dump", func() {
		row := externalRole{models.ExternalRole{
			Name: "admin", RoleType: "ClusterRole", Description: "cluster admin", Tenant: "acme", CreatedAt: created,
		}}

		out, err := formatters.NewCSVFormatter().Format([]externalRole{row}, formatters.FormatOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(strings.TrimSpace(out), "\n")).To(Equal([]string{
			"Name,Type,Description,Tenant,Created",
			"admin,ClusterRole,cluster admin,acme,2.0h",
		}))
	})
})

var _ = ginkgo.Describe("wantsFlatRows", func() {
	ginkgo.It("is true for the formats whose formatter flattens a grouped Pretty value", func() {
		Expect(wantsFlatRows(clicky.FormatOptions{CSV: true})).To(BeTrue())
		Expect(wantsFlatRows(clicky.FormatOptions{Markdown: true})).To(BeTrue())
		Expect(wantsFlatRows(clicky.FormatOptions{HTML: true})).To(BeTrue())
		Expect(wantsFlatRows(clicky.FormatOptions{Format: "csv"})).To(BeTrue())
		Expect(wantsFlatRows(clicky.FormatOptions{Format: "md"})).To(BeTrue())
		Expect(wantsFlatRows(clicky.FormatOptions{Format: "json=out.json,csv"})).To(BeTrue())
	})

	ginkgo.It("is false for the formats that keep the grouping", func() {
		Expect(wantsFlatRows(clicky.FormatOptions{})).To(BeFalse())
		Expect(wantsFlatRows(clicky.FormatOptions{JSON: true})).To(BeFalse())
		Expect(wantsFlatRows(clicky.FormatOptions{Format: "yaml"})).To(BeFalse())
		Expect(wantsFlatRows(clicky.FormatOptions{Format: "pretty"})).To(BeFalse())
	})

	ginkgo.It("leaves the caller's options untouched", func() {
		opts := clicky.FormatOptions{Format: "csv"}

		Expect(wantsFlatRows(opts)).To(BeTrue())
		Expect(opts.Sinks).To(BeEmpty())
	})
})
