package actions

import (
	"fmt"
	"testing"

	"github.com/flanksource/commons/logger"
	"github.com/fluxcd/pkg/gittestserver"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/samber/oops"
)

var (
	gitServer *gittestserver.GitServer
)

func init() {
	format.RegisterCustomFormatter(func(value interface{}) (string, bool) {

		if e, ok := value.(error); ok {
			if err, ok := oops.AsOops(e); ok {
				return fmt.Sprintf("%+v", err), true
			}
		}
		return "", false
	})
}

func TestPlaybookActions(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Playbook Action")
}

var _ = ginkgo.BeforeSuite(func() {
	var err error
	gitServer, err = gittestserver.NewTempGitServer()
	Expect(err).NotTo(HaveOccurred())

	logger.Infof("Git server started at: %s", gitServer.Root())

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := gitServer.StartHTTP(); err != nil {
			ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
		}
	}()
})

var _ = ginkgo.AfterSuite(func() {
	gitServer.StopHTTP()
})
