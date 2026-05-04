package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("facet report package extraction", func() {
	ginkgo.It("includes clicky-ui runtime dependencies in the embedded manifest", func() {
		data, err := FS.ReadFile("package.json")
		Expect(err).NotTo(HaveOccurred())

		var manifest map[string]any
		Expect(json.Unmarshal(data, &manifest)).To(Succeed())
		dependencies := manifest["dependencies"].(map[string]any)

		for _, name := range []string{
			"@radix-ui/react-compose-refs",
			"@radix-ui/react-slot",
			"class-variance-authority",
			"clsx",
			"lucide-react",
			"marked",
			"tailwind-merge",
		} {
			Expect(dependencies).To(HaveKey(name))
		}
	})

	ginkgo.It("includes stdout and stderr in facet command errors", func() {
		err := facetCommandError("pdf", os.ErrInvalid, "out line\n", "err line\n")

		Expect(err.Error()).To(ContainSubstring("facet pdf failed"))
		Expect(err.Error()).To(ContainSubstring("stdout:\nout line"))
		Expect(err.Error()).To(ContainSubstring("stderr:\nerr line"))
	})

	ginkgo.It("extracts a package manifest without broken relative link overrides", func() {
		dir := ginkgo.GinkgoT().TempDir()

		Expect(ExtractFiles(dir)).To(Succeed())

		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		Expect(err).NotTo(HaveOccurred())

		var manifest map[string]any
		Expect(json.Unmarshal(data, &manifest)).To(Succeed())
		assertNoBrokenLocalRefs(manifest)
	})

	ginkgo.It("removes unavailable local pnpm overrides while preserving registry dependencies", func() {
		data := []byte(`{
  "dependencies": {
    "@flanksource/facet": "^0.1.38",
    "@flanksource/clicky-ui": "^0.1.0"
  },
  "pnpm": {
    "overrides": {
      "@flanksource/facet": "link:../../facet",
      "@flanksource/clicky-ui": "file:/definitely/not/clicky-ui",
      "react": "^19.2.0"
    }
  }
}`)

		sanitized, err := sanitizeReportPackageJSON(data, true)
		Expect(err).NotTo(HaveOccurred())

		var manifest map[string]any
		Expect(json.Unmarshal(sanitized, &manifest)).To(Succeed())
		Expect(manifest["dependencies"]).To(HaveKeyWithValue("@flanksource/facet", "^0.1.38"))
		Expect(manifest["dependencies"]).To(HaveKeyWithValue("@flanksource/clicky-ui", "^0.1.0"))
		assertNoBrokenLocalRefs(manifest)
	})

	ginkgo.It("strips local pnpm overrides for remote render archives", func() {
		data := []byte(`{
  "dependencies": {
    "@flanksource/facet": "^0.1.38"
  },
  "pnpm": {
    "overrides": {
      "@flanksource/facet": "link:../../facet"
    }
  }
}`)

		sanitized, err := sanitizeReportPackageJSON(data, false)
		Expect(err).NotTo(HaveOccurred())

		var manifest map[string]any
		Expect(json.Unmarshal(sanitized, &manifest)).To(Succeed())
		Expect(manifest["dependencies"]).To(HaveKeyWithValue("@flanksource/facet", "^0.1.38"))
		Expect(manifest).NotTo(HaveKey("pnpm"))
	})

	ginkgo.It("clears stale facet install state with broken local links", func() {
		dir := ginkgo.GinkgoT().TempDir()
		flanksourceDir := filepath.Join(dir, ".facet", "node_modules", "@flanksource")
		Expect(os.MkdirAll(flanksourceDir, 0750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, ".facet", "package.json"), []byte("{}"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, ".facet", "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'"), 0600)).To(Succeed())
		Expect(os.Symlink(filepath.Join(dir, "missing-facet"), filepath.Join(flanksourceDir, "facet"))).To(Succeed())

		Expect(ExtractFiles(dir)).To(Succeed())

		Expect(filepath.Join(dir, "pnpm-lock.yaml")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(dir, ".facet", "package.json")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(dir, ".facet", "pnpm-lock.yaml")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(dir, ".facet", "node_modules")).NotTo(BeADirectory())
	})
})

func assertNoBrokenLocalRefs(manifest map[string]any) {
	pnpm, ok := manifest["pnpm"].(map[string]any)
	if !ok {
		return
	}
	overrides, ok := pnpm["overrides"].(map[string]any)
	if !ok {
		return
	}
	for _, raw := range overrides {
		value, ok := raw.(string)
		if !ok || !isLocalPackageRef(value) {
			continue
		}
		_, path, ok := strings.Cut(value, ":")
		Expect(ok).To(BeTrue())
		Expect(filepath.IsAbs(path)).To(BeTrue())
		Expect(pathExists(path)).To(BeTrue())
	}
}
