package auth

import (
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/rbac"
)

var _ = ginkgo.Describe("InvalidateRLSCacheForUser", func() {
	ginkgo.It("removes the payloads cached per access token along with the base payload", func() {
		const subject = "federated:3f1c0000-0000-0000-0000-000000000001"
		const other = subject + "2"

		keys := []string{
			getRLSCacheKey(subject),
			getRLSCacheKey(subject) + rbac.AccessTokenIDCacheKey(uuid.New()),
			getRLSCacheKey(other),
			getRLSCacheKey(other) + rbac.AccessTokenIDCacheKey(uuid.New()),
		}
		for _, key := range keys {
			tokenCache.SetDefault(key, &rls.Payload{})
		}
		ginkgo.DeferCleanup(func() {
			for _, key := range keys {
				tokenCache.Delete(key)
			}
		})

		InvalidateRLSCacheForUser(subject)

		for _, key := range keys[:2] {
			_, found := tokenCache.Get(key)
			Expect(found).To(BeFalse(), key)
		}
		for _, key := range keys[2:] {
			_, found := tokenCache.Get(key)
			Expect(found).To(BeTrue(), key)
		}
	})
})
