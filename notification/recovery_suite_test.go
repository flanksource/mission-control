//go:build recoverytests

package notification_test

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"strings"
)

// This explicit test configuration isolates the v2 adapter from legacy specs and consumers.
func init() { recoveryOnlySuite = true }

var _ = ginkgo.BeforeEach(func() {
	if !strings.HasPrefix(ginkgo.CurrentSpecReport().FullText(), "Notification recovery") {
		ginkgo.Skip("run legacy notification specs without -tags=recoverytests")
	}
})
