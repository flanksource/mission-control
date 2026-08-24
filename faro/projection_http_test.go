package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// httpProjectionManifest is the shape a register repository actually writes: a
// templated URL over the target's own entries.
func httpProjectionManifest(dir, targetPath, url string, extra string) Projection {
	manifest := writeProjectionTestFile(dir, "projection.yaml", fmt.Sprintf(`apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: repository-languages
spec:
  source:
    query:
      http:
        url: %s
%s
  target:
    path: %s
    select: $.entries[*]
    create: false
  match:
    - source.repository == target.repository
  set:
    $.code_volume:
      value: 'source.body'
      when: '"body" in source'
`, url, extra, targetPath))

	projections, err := loadProjections(manifest, projectionLoadOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(projections).To(HaveLen(1))
	return projections[0]
}

func writeHTTPTargetFile(dir string, repositories ...string) string {
	body := "schema_version: 2\nentries:\n"
	for _, repository := range repositories {
		body += fmt.Sprintf("  - repository: %s\n    owner: Repository Maintainer\n", repository)
	}
	return writeProjectionTestFile(dir, "target.yaml", body)
}

var _ = ginkgo.Describe("HTTP projection source", func() {
	ginkgo.It("decodes an inline HTTPConnection under strict field checking", func() {
		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, "https://api.github.com/repos/{{.repository}}/languages", `        method: GET
        bearer:
          value: $FARO_TEST_TOKEN
        headers:
          - name: Accept
            value: application/vnd.github+json`)

		query := projection.Spec.Source.Query.HTTP
		Expect(query).ToNot(BeNil())
		Expect(query.URL).To(Equal("https://api.github.com/repos/{{.repository}}/languages"))
		Expect(query.Method).To(Equal("GET"))
		Expect(query.Bearer.ValueStatic).To(Equal("$FARO_TEST_TOKEN"))
		Expect(query.Headers).To(HaveLen(1))
		Expect(query.Headers[0].Name).To(Equal("Accept"))
		Expect(projection.validate()).To(Succeed())
	})

	ginkgo.It("issues one request per target entry, in register order", func() {
		var mutex sync.Mutex
		var requested []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutex.Lock()
			requested = append(requested, r.URL.Path)
			mutex.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"Go": 19816, "YAML": 452}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/commons", "flanksource/duty")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repository}}/languages", "")

		items, warnings, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		Expect(warnings).To(BeEmpty())

		Expect(requested).To(Equal([]string{
			"/repos/flanksource/commons/languages",
			"/repos/flanksource/duty/languages",
		}))
		Expect(items).To(HaveLen(2))
		// The entry's own scalars ride along so spec.match reads like every other kind.
		Expect(items[0]["repository"]).To(Equal("flanksource/commons"))
		Expect(items[0]["body"]).To(Equal(map[string]any{"Go": float64(19816), "YAML": float64(452)}))
	})

	ginkgo.It("requests only the entries spec.target.filter scopes it to", func() {
		var mutex sync.Mutex
		var requested []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutex.Lock()
			requested = append(requested, r.URL.Path)
			mutex.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Go": 1}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeProjectionTestFile(dir, "target.yaml", `schema_version: 2
entries:
  - repository: flanksource/commons
    kind: repository
  - repository: acme/not-ours
    kind: vendor
`)
		manifest := writeProjectionTestFile(dir, "projection.yaml", fmt.Sprintf(`apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: scoped
spec:
  source:
    query:
      http:
        url: %s/repos/{{.repository}}/languages
  target:
    path: %s
    select: $.entries[*]
    filter: 'target.kind == "repository"'
    create: false
  match:
    - source.repository == target.repository
  set:
    $.code_volume:
      value: 'source.body'
`, server.URL, target))

		projections, err := loadProjections(manifest, projectionLoadOptions{})
		Expect(err).ToNot(HaveOccurred())

		items, _, err := queryHTTPProjection(projections[0], *projections[0].Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		// The vendor entry is never requested — it is not this projection's to read.
		Expect(requested).To(Equal([]string{"/repos/flanksource/commons/languages"}))
		Expect(items).To(HaveLen(1))
	})

	ginkgo.It("waits out a 202 and takes the answer that follows", func() {
		var mutex sync.Mutex
		attempts := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutex.Lock()
			attempts++
			current := attempts
			mutex.Unlock()

			if current < 3 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"total": 7, "week": 1787000000}]`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repository}}/stats", `        accepted:
          attempts: 5
          wait: 1ms`)

		items, warnings, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		Expect(warnings).To(BeEmpty())
		Expect(attempts).To(Equal(3))
		Expect(items).To(HaveLen(1))
		Expect(items[0]["body"]).To(HaveLen(1))
	})

	ginkgo.It("yields no row when the statistics never finish computing", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repository}}/stats", `        accepted:
          attempts: 2
          wait: 1ms`)

		items, warnings, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		// No row at all, so every mapping is skipped and the register keeps no key.
		// A zero here would state that the repository had no commits.
		Expect(items).To(BeEmpty())
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0].Message).To(ContainSubstring("still computing"))
	})

	ginkgo.It("drops the entry and warns when the API refuses, without failing the run", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repos/flanksource/private/languages" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Go": 10}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/private", "flanksource/public")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repository}}/languages", "")

		items, warnings, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(items[0]["repository"]).To(Equal("flanksource/public"))
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0].Source).To(ContainSubstring("flanksource/private"))
	})

	ginkgo.It("sends the bearer token expanded from the environment", func() {
		var mutex sync.Mutex
		var seen string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutex.Lock()
			seen = r.Header.Get("Authorization")
			mutex.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		}))
		defer server.Close()

		ginkgo.GinkgoT().Setenv("FARO_TEST_TOKEN", "ghp-example")
		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repository}}", `        bearer:
          value: $FARO_TEST_TOKEN`)

		_, _, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		Expect(seen).To(Equal("Bearer ghp-example"))
	})

	ginkgo.It("refuses a URL naming a field the register does not carry", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, server.URL+"/repos/{{.repo_name}}", "")

		_, _, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("repo_name"))
	})

	ginkgo.It("catches a URL field the register lacks during verify, with no request", func() {
		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		// example.test is reserved and unroutable: if verify reached the network
		// this would hang or fail on dial rather than on the field name.
		projection := httpProjectionManifest(dir, target, "https://example.test/{{.repo_name}}", "")

		err := verifyProjection(projection)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("repo_name"))
	})

	ginkgo.It("refuses credentials it cannot resolve rather than sending none", func() {
		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, "https://example.test/{{.repository}}", `        bearer:
          valueFrom:
            secretKeyRef:
              name: github
              key: token`)

		_, _, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("valueFrom is not supported"))
	})

	ginkgo.It("rejects connection features faro cannot honour, at load time", func() {
		manifest := writeProjectionTestFile(ginkgo.GinkgoT().TempDir(), "projection.yaml", `apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: connection-backed
spec:
  source:
    query:
      http:
        url: https://example.test/{{.repository}}
        connection: github
  target:
    path: target.yaml
    select: $.entries[*]
  match:
    - source.repository == target.repository
  set:
    $.code_volume:
      value: 'source.body'
`)
		_, err := loadProjections(manifest, projectionLoadOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("connection is not supported"))
	})

	ginkgo.It("requires a target, since the target's entries are the rows", func() {
		manifest := writeProjectionTestFile(ginkgo.GinkgoT().TempDir(), "projection.yaml", `apiVersion: faro.flanksource.com/v1alpha1
kind: Projection
metadata:
  name: no-target
spec:
  source:
    query:
      http:
        url: https://example.test/repos
`)
		_, err := loadProjections(manifest, projectionLoadOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("requires spec.target"))
	})

	ginkgo.It("selects a nested payload when the API wraps it", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data": {"languages": {"Go": 5}}}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/clicky")
		projection := httpProjectionManifest(dir, target, server.URL+"/{{.repository}}", `        select: $.data.languages`)

		items, _, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())
		Expect(items[0]["body"]).To(Equal(map[string]any{"Go": float64(5)}))
	})

	ginkgo.It("applies through the engine without the apply step knowing the kind", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Go": 19816}`)
		}))
		defer server.Close()

		dir := ginkgo.GinkgoT().TempDir()
		target := writeHTTPTargetFile(dir, "flanksource/commons")
		projection := httpProjectionManifest(dir, target, server.URL+"/{{.repository}}", "")

		items, warnings, err := queryHTTPProjection(projection, *projection.Spec.Source.Query.HTTP)
		Expect(err).ToNot(HaveOccurred())

		result, err := applyProjection(projection, projectionSourceResult{
			Context:  map[string]any{"name": "test"},
			Items:    items,
			Warnings: warnings,
		}, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Changed).To(HaveLen(1))

		written, err := os.ReadFile(filepath.Join(dir, "target.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(written)).To(ContainSubstring("code_volume"))
		Expect(string(written)).To(ContainSubstring("19816"))
	})
})
