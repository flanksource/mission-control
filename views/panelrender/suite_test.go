package panelrender_test

import (
	"os/exec"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPanelRender(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Panel Render")
}

var _ = ginkgo.BeforeSuite(func() {
	if _, err := exec.LookPath("facet"); err != nil {
		ginkgo.Fail("facet binary not found on PATH; install with: npm install -g @flanksource/facet")
	}
})
