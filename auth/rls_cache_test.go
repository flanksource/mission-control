package auth

import (
	"github.com/flanksource/duty/rls"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("InvalidateRLSCacheForUser", func() {
	ginkgo.It("removes the payloads cached per scope header along with the base payload", func() {
		const subject = "federated:3f1c0000-0000-0000-0000-000000000001"
		const other = subject + "2"

		keys := []string{
			getRLSCacheKey(subject),
			getRLSCacheKey(subject) + ":scope-prod",
			getRLSCacheKey(other),
			getRLSCacheKey(other) + ":scope-prod",
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
