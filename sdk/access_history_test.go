package sdk

import (
	"context"
	"fmt"
	"net/url"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("access history", func() {
	ginkgo.It("embeds the acting user and hydrates config identity on logs", func() {
		seen := map[string]url.Values{}
		since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		server := pgRoutes(map[string]string{
			"config_access_logs": fmt.Sprintf(`[{"config_id":%q,"external_user_id":%q,"created_at":"2026-06-01T00:00:00Z","mfa":true,"count":4,"external_users":{"name":"jane","user_email":"jane@example.com"}}]`, config1, userOne),
			"config_items":       fmt.Sprintf(`[{"id":%q,"name":"prod-app","type":"Azure::EnterpriseApplication"}]`, config1),
		}, seen)
		defer server.Close()

		logs, _, err := New(server.URL, "tok").ListAccessLogs(context.Background(), AccessHistoryOptions{
			ConfigIDs: []string{config1},
			UserIDs:   []string{userOne},
			Since:     &since,
			Limit:     100,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(logs).To(HaveLen(1))
		Expect(logs[0].UserName()).To(Equal("jane"))
		Expect(logs[0].UserEmail()).To(Equal("jane@example.com"))
		Expect(logs[0].ConfigName).To(Equal("prod-app"))
		Expect(logs[0].ConfigType).To(Equal("Azure::EnterpriseApplication"))
		Expect(logs[0].MFA).To(BeTrue())

		q := seen["config_access_logs"]
		Expect(q.Get("select")).To(Equal(accessLogSelect))
		Expect(q.Get("order")).To(Equal("created_at.desc"))
		Expect(q.Get("config_id")).To(Equal("in.(" + config1 + ")"))
		Expect(q.Get("external_user_id")).To(Equal("in.(" + userOne + ")"))
		Expect(q.Get("created_at")).To(Equal("gte.2026-05-01T00:00:00Z"))
		Expect(q.Get("limit")).To(Equal("100"))
	})

	ginkgo.It("falls back to the user id when the embedded user is gone", func() {
		server := pgRoutes(map[string]string{
			"config_access_logs": fmt.Sprintf(`[{"config_id":%q,"external_user_id":%q,"created_at":"2026-06-01T00:00:00Z"}]`, config1, userOne),
			"config_items":       `[]`,
		}, nil)
		defer server.Close()

		logs, _, err := New(server.URL, "tok").ListAccessLogs(context.Background(), AccessHistoryOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(logs[0].UserName()).To(Equal(userOne))
		Expect(logs[0].UserEmail()).To(BeEmpty())
	})

	ginkgo.It("resolves config, principal and role names onto reviews", func() {
		server := pgRoutes(map[string]string{
			"access_reviews":  fmt.Sprintf(`[{"config_id":%q,"external_user_id":%q,"external_role_id":%q,"source":"UI","created_at":"2026-06-01T00:00:00Z"}]`, config1, userOne, roleOne),
			"config_items":    fmt.Sprintf(`[{"id":%q,"name":"prod-app","type":"Azure::EnterpriseApplication"}]`, config1),
			"external_users":  fmt.Sprintf(`[{"id":%q,"name":"jane"}]`, userOne),
			"external_roles":  fmt.Sprintf(`[{"id":%q,"name":"Owner"}]`, roleOne),
			"external_groups": `[]`,
		}, nil)
		defer server.Close()

		reviews, _, err := New(server.URL, "tok").ListAccessReviews(context.Background(), AccessHistoryOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(reviews).To(HaveLen(1))
		Expect(reviews[0].ConfigName).To(Equal("prod-app"))
		Expect(reviews[0].User).To(Equal("jane"))
		Expect(reviews[0].Role).To(Equal("Owner"))
		Expect(reviews[0].Source).To(Equal("UI"))
	})

	ginkgo.It("names a group-held review after the group", func() {
		server := pgRoutes(map[string]string{
			"access_reviews":  fmt.Sprintf(`[{"config_id":%q,"external_group_id":%q,"external_role_id":%q,"source":"UI","created_at":"2026-06-01T00:00:00Z"}]`, config1, groupA, roleOne),
			"config_items":    `[]`,
			"external_roles":  fmt.Sprintf(`[{"id":%q,"name":"Reader"}]`, roleOne),
			"external_groups": fmt.Sprintf(`[{"id":%q,"name":"sre-team"}]`, groupA),
		}, nil)
		defer server.Close()

		reviews, _, err := New(server.URL, "tok").ListAccessReviews(context.Background(), AccessHistoryOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(reviews[0].User).To(Equal("sre-team"))
		Expect(reviews[0].Role).To(Equal("Reader"))
	})
})
