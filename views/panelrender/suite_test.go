package panelrender_test

import (
	"os/exec"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPanelRender(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Panel Render", ginkgo.Label("ignore_local"))
}

var _ = ginkgo.BeforeSuite(func() {
	// The facet render pipeline currently fails inside its generated .facet/
	// Vite build (PostCSS config load error). Skip until a fixed facet release
	// is available.
	ginkgo.Skip("disabled: facet Vite build is broken (PostCSS config load failure)")

	if _, err := exec.LookPath("facet"); err != nil {
		ginkgo.Fail("facet binary not found on PATH; install with: npm install -g @flanksource/facet")
	}
})
