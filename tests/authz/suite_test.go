package authz_test

import (
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/tests/authz/harness"
)

func TestAuthz(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Authorization end-to-end")
}

var server *harness.Server

var _ = ginkgo.BeforeSuite(func() {
	// AUTHZ_DIR keeps Mission Control's log, database and htpasswd file after the run, to look into a failure
	dir := os.Getenv("AUTHZ_DIR")
	if dir == "" {
		dir = ginkgo.GinkgoT().TempDir()
	}

	var err error
	server, err = harness.StartServer(dir, harness.UsersNeeded())
	Expect(err).ToNot(HaveOccurred())
	harness.Use(server)
})

var _ = ginkgo.AfterSuite(func() {
	if server != nil {
		server.Stop()
	}
})

var _ = ginkgo.ReportAfterSuite("mission control log", func(report ginkgo.Report) {
	if !report.SuiteSucceeded && server != nil {
		ginkgo.GinkgoWriter.Println(server.LogTail(200))
	}
})
