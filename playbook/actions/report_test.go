package actions

import (
	gocontext "context"
	"os"
	"path/filepath"

	commons "github.com/flanksource/commons/context"
	"github.com/flanksource/duty/context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("Report action source resolution", func() {
	ctx := context.Context{Context: commons.NewContext(gocontext.TODO())}

	ginkgo.It("defaults to the embedded CatalogReport.tsx", func() {
		srcDir, entry, err := resolveReportSource(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(entry).To(Equal("CatalogReport.tsx"))
		Expect(srcDir).NotTo(BeEmpty())
		Expect(filepath.Join(srcDir, entry)).To(BeAnExistingFile())
	})

	ginkgo.It("resolves an absolute local path as-is", func() {
		dir, err := os.MkdirTemp("", "report-abs-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(dir)

		abs := filepath.Join(dir, "MyReport.tsx")
		Expect(os.WriteFile(abs, []byte("export default function R() { return null }"), 0600)).To(Succeed())

		srcDir, entry, err := resolveReportSource(ctx, &v1.ReportFile{Path: abs})
		Expect(err).NotTo(HaveOccurred())
		Expect(srcDir).To(Equal(dir))
		Expect(entry).To(Equal("MyReport.tsx"))
	})

	ginkgo.It("resolves a relative local path against the working directory", func() {
		cwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		dir, err := os.MkdirTemp(cwd, "report-rel-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(dir)

		abs := filepath.Join(dir, "RelReport.tsx")
		Expect(os.WriteFile(abs, []byte("export default function R() { return null }"), 0600)).To(Succeed())

		rel, err := filepath.Rel(cwd, abs)
		Expect(err).NotTo(HaveOccurred())

		srcDir, entry, err := resolveReportSource(ctx, &v1.ReportFile{Path: rel})
		Expect(err).NotTo(HaveOccurred())
		Expect(srcDir).To(Equal(dir))
		Expect(entry).To(Equal("RelReport.tsx"))
	})

	ginkgo.It("errors when a local path does not exist", func() {
		_, _, err := resolveReportSource(ctx, &v1.ReportFile{Path: "/does/not/exist/Report.tsx"})
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("clones a git source and resolves the file within it", ginkgo.Label("slow"), func() {
		err := gitServer.InitRepo("testdata/report-repo", "main", "report-repo")
		Expect(err).NotTo(HaveOccurred())

		srcDir, entry, err := resolveReportSource(ctx, &v1.ReportFile{
			Git: &v1.ReportGitFile{
				URL:  gitServer.HTTPAddress() + "/report-repo",
				Base: "main",
				File: "CustomReport.tsx",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(entry).To(Equal("CustomReport.tsx"))
		Expect(filepath.Join(srcDir, entry)).To(BeAnExistingFile())
	})
})
