package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	userThree = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	groupC    = "33333333-3333-3333-3333-333333333333"
	roleTwo   = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

type identityResponses struct {
	candidates string
	selected   string
}

func identityRoutes(responses map[string]identityResponses, seen map[string][]url.Values) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		table := strings.TrimPrefix(r.URL.Path, "/db/")
		response, ok := responses[table]
		if !ok {
			ginkgo.Fail("unexpected request: " + r.URL.Path)
		}
		seen[table] = append(seen[table], r.URL.Query())
		body := response.selected
		if r.URL.Query().Get("select") != "" {
			body = response.candidates
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

var _ = ginkgo.Describe("MatchItem access filters", func() {
	ginkgo.It("passes scalar grant patterns through filter query arguments", func() {
		seen := map[string]url.Values{}
		server := pgRoutes(map[string]string{
			"config_access_summary":           `[]`,
			"config_access_summary_by_config": `[]`,
		}, seen)
		defer server.Close()

		client := New(server.URL, "tok")
		filters := AccessGrantOptions{
			ConfigType: "Azure::*,!Azure::Legacy*",
			Role:       "Owner,Reader*",
			UserType:   "Human,!Service*",
		}
		_, _, err := client.ListAccessGrants(context.Background(), filters)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = client.ListAccessSummaryByConfig(context.Background(), filters)
		Expect(err).ToNot(HaveOccurred())

		grantsQuery := seen["config_access_summary"]
		Expect(grantsQuery.Get("config_type.filter")).To(Equal(filters.ConfigType))
		Expect(grantsQuery.Get("role.filter")).To(Equal(filters.Role))
		Expect(grantsQuery.Get("user_type.filter")).To(Equal(filters.UserType))
		Expect(grantsQuery.Get("config_type")).To(BeEmpty())
		Expect(grantsQuery.Get("role")).To(BeEmpty())
		Expect(grantsQuery.Get("user_type")).To(BeEmpty())
		Expect(seen["config_access_summary_by_config"].Get("config_type.filter")).To(Equal(filters.ConfigType))
	})

	ginkgo.It("matches identity fields before applying the limit", func() {
		seen := map[string][]url.Values{}
		server := identityRoutes(map[string]identityResponses{
			"external_users": {
				candidates: fmt.Sprintf(`[
					{"id":%q,"name":"Alice","email":"ALICE@example.com","aliases":[]},
					{"id":%q,"name":"Jane","email":"jane@corp.example","aliases":["jdoe"]},
					{"id":%q,"name":"svc-reader","email":"reader@example.com","aliases":[]}
				]`, userTwo, userOne, userThree),
				selected: fmt.Sprintf(`[{"id":%q,"name":"Alice","email":"ALICE@example.com","user_type":"Human"}]`, userTwo),
			},
			"external_group_summary": {
				candidates: fmt.Sprintf(`[
					{"id":%q,"name":"Platform","aliases":["ops-team"]},
					{"id":%q,"name":"ops-retired","aliases":[]}
				]`, groupA, groupC),
				selected: fmt.Sprintf(`[{"id":%q,"name":"Platform","aliases":["ops-team"],"members_count":8}]`, groupA),
			},
			"external_roles": {
				candidates: fmt.Sprintf(`[
					{"id":%q,"name":"Owner","aliases":[]},
					{"id":%q,"name":"Auditor","aliases":["Owner"]}
				]`, roleOne, roleTwo),
				selected: fmt.Sprintf(`[{"id":%q,"name":"Owner"}]`, roleOne),
			},
		}, seen)
		defer server.Close()

		client := New(server.URL, "tok")
		users, userTotal, err := client.ListExternalUsers(context.Background(), IdentityOptions{
			Name: "jane,*@example.com,!svc-*", Type: "Human,!Service*", Limit: 1,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(users).To(HaveLen(1))
		Expect(users[0].Name).To(Equal("Alice"))
		Expect(userTotal).To(Equal(2))

		groups, groupTotal, err := client.ListExternalGroups(context.Background(), IdentityOptions{
			Name: "ops*,!ops-retired", Type: "Security*",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(groups).To(HaveLen(1))
		Expect(groups[0].Name).To(Equal("Platform"))
		Expect(groupTotal).To(Equal(1))

		roles, roleTotal, err := client.ListExternalRoles(context.Background(), IdentityOptions{Name: "owner"})
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).To(HaveLen(1))
		Expect(roles[0].Name).To(Equal("Owner"))
		Expect(roleTotal).To(Equal(1))

		Expect(seen["external_users"]).To(HaveLen(2))
		Expect(seen["external_users"][0].Get("select")).To(Equal("id,name,email,aliases"))
		Expect(seen["external_users"][0].Get("limit")).To(BeEmpty())
		Expect(seen["external_users"][0].Get("user_type.filter")).To(Equal("Human,!Service*"))
		Expect(seen["external_users"][1].Get("id")).To(Equal("in.(" + userTwo + ")"))
		Expect(seen["external_group_summary"][0].Get("select")).To(Equal("id,name,aliases"))
		Expect(seen["external_roles"][0].Get("select")).To(Equal("id,name"))
	})
})
