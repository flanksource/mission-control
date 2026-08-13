package main

import (
	"context"
	"fmt"

	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("access MatchItem arguments", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("resolves every user matching the filter", func() {
		server := accessServer(map[string]string{
			"external_users": fmt.Sprintf(`[
				{"id":%q,"name":"Jane Doe","email":"jane@example.com"},
				{"id":%q,"name":"Janet Doe","email":"janet@example.com"}
			]`, testUserID, testUser2ID),
		}, nil)
		defer server.Close()

		ids, err := resolveUserIDs(context.Background(), sdk.New(server.URL, "tok"), "Jane*")

		Expect(err).ToNot(HaveOccurred())
		Expect(ids).To(ConsistOf(testUserID, testUser2ID))
	})

	ginkgo.It("returns an empty match set without falling through to unfiltered access queries", func() {
		server := accessServer(map[string]string{"external_users": `[]`}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		permissionsUser = "missing*"
		permissionsByUser = false
		permissionsByConfig = false
		permissionsExpandGroups = false
		permissionsLimit = 0
		accessLogsUser = "missing*"
		accessLogsSince = ""
		accessLogsLimit = 100
		accessReviewsUser = "missing*"
		accessReviewsSince = ""
		accessReviewsLimit = 100
		ginkgo.DeferCleanup(func() {
			permissionsUser = ""
			accessLogsUser = ""
			accessLogsSince = "90d"
			accessReviewsUser = ""
			accessReviewsSince = "90d"
		})

		Expect(AccessPermissions.RunE(AccessPermissions, nil)).To(Succeed())
		Expect(AccessLogs.RunE(AccessLogs, nil)).To(Succeed())
		Expect(AccessReviews.RunE(AccessReviews, nil)).To(Succeed())
	})

	ginkgo.It("keeps shell completion as a prefix search", func() {
		server := accessServer(map[string]string{
			"external_users":         fmt.Sprintf(`[{"id":%q,"name":"Jane"}]`, testUserID),
			"external_group_summary": fmt.Sprintf(`[{"id":%q,"name":"sre-team"}]`, testGroupID),
			"external_roles":         fmt.Sprintf(`[{"id":%q,"name":"Owner"}]`, testRoleID),
		}, nil)
		defer server.Close()
		storeRemoteContext(server.URL)

		users, _ := completeAccessUserIDs(nil, nil, "Ja")
		groups, _ := completeAccessGroupIDs(nil, nil, "sre")
		roles, _ := completeAccessRoleIDs(nil, nil, "Ow")

		Expect(users).To(Equal([]string{testUserID}))
		Expect(groups).To(Equal([]string{testGroupID}))
		Expect(roles).To(Equal([]string{testRoleID}))
	})
})
