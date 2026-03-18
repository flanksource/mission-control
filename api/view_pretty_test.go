package api_test

import (
	"encoding/json"

	clickyAPI "github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/view"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/flanksource/incident-commander/api"
)

var _ = ginkgo.Describe("SerializedSection Pretty", func() {
	ginkgo.It("empty section returns empty text", func() {
		s := api.SerializedSection{}
		Expect(s.Pretty().String()).To(BeEmpty())
	})

	ginkgo.It("title-only section includes heading", func() {
		s := api.SerializedSection{Title: "Overview"}
		Expect(s.Pretty().String()).To(ContainSubstring("Overview"))
	})

	ginkgo.It("data rows produce table with correct headers and values", func() {
		s := api.SerializedSection{
			Data: []map[string]any{
				{"name": "db", "status": "healthy"},
				{"name": "cache", "status": "degraded"},
			},
		}
		html := s.Pretty().HTML()
		// Column headers are prettified to Title Case by clicky
		Expect(html).To(ContainSubstring("Name"))
		Expect(html).To(ContainSubstring("Status"))
		Expect(html).To(ContainSubstring("db"))
		Expect(html).To(ContainSubstring("healthy"))
		Expect(html).To(ContainSubstring("cache"))
		Expect(html).To(ContainSubstring("degraded"))
	})

	ginkgo.It("variables produce description list items", func() {
		s := api.SerializedSection{
			Variables: map[string]string{"env": "production", "region": "us-east-1"},
		}
		html := s.Pretty().HTML()
		Expect(html).To(ContainSubstring("env"))
		Expect(html).To(ContainSubstring("production"))
		Expect(html).To(ContainSubstring("region"))
		Expect(html).To(ContainSubstring("us-east-1"))
	})

	ginkgo.It("Body badge content appears in HTML output", func() {
		s := api.SerializedSection{
			Title: "Broken",
			Body:  clickyAPI.Text{}.Add(clickyAPI.Badge("Error: failed to run view", "text-red-700", "bg-red-100")),
		}
		Expect(s.Pretty().HTML()).To(ContainSubstring("Error: failed to run view"))
	})
})

var _ = ginkgo.Describe("SerializedView Pretty", func() {
	ginkgo.It("includes namespace/name title", func() {
		v := api.SerializedView{
			Name:      "my-view",
			Namespace: "default",
		}
		Expect(v.Pretty().String()).To(ContainSubstring("default/my-view"))
	})

	ginkgo.It("name-only title when namespace is empty", func() {
		v := api.SerializedView{Name: "my-view"}
		Expect(v.Pretty().String()).To(ContainSubstring("my-view"))
		Expect(v.Pretty().String()).ToNot(ContainSubstring("/my-view"))
	})

	ginkgo.It("includes all sub-section titles", func() {
		v := api.SerializedView{
			Name: "top",
			Section: []api.SerializedSection{
				{Title: "Alpha"},
				{Title: "Beta"},
			},
		}
		html := v.Pretty().HTML()
		Expect(html).To(ContainSubstring("Alpha"))
		Expect(html).To(ContainSubstring("Beta"))
	})
})

var _ = ginkgo.Describe("SerializedSection MarshalJSON", func() {
	ginkgo.It("omits Filters and Variables, keeps Title/Icon/Data", func() {
		s := api.SerializedSection{
			Title:     "Info",
			Icon:      "server",
			Data:      []map[string]any{{"key": "val"}},
			Variables: map[string]string{"x": "1"},
			Filters: map[string]api.ColumnFilterOptions{
				"col": {List: []string{"a", "b"}},
			},
		}
		b, err := json.Marshal(s)
		Expect(err).ToNot(HaveOccurred())

		var out map[string]any
		Expect(json.Unmarshal(b, &out)).To(Succeed())
		Expect(out).To(HaveKey("title"))
		Expect(out).To(HaveKey("icon"))
		Expect(out).To(HaveKey("data"))
		Expect(out).ToNot(HaveKey("variables"))
		Expect(out).ToNot(HaveKey("filters"))
	})

	ginkgo.It("omits empty fields", func() {
		s := api.SerializedSection{Title: "Only"}
		b, err := json.Marshal(s)
		Expect(err).ToNot(HaveOccurred())

		var out map[string]any
		Expect(json.Unmarshal(b, &out)).To(Succeed())
		Expect(out).To(HaveKey("title"))
		Expect(out).ToNot(HaveKey("icon"))
		Expect(out).ToNot(HaveKey("data"))
	})

	ginkgo.It("includes non-empty Body in JSON output", func() {
		s := api.SerializedSection{
			Title: "Broken",
			Body:  clickyAPI.Text{}.Add(clickyAPI.Badge("Error: failed to run view", "text-red-700", "bg-red-100")),
		}
		b, err := json.Marshal(s)
		Expect(err).ToNot(HaveOccurred())

		var out map[string]any
		Expect(json.Unmarshal(b, &out)).To(Succeed())
		Expect(out).To(HaveKey("body"))
	})
})

var _ = ginkgo.Describe("ViewResult.Serialized", func() {
	ginkgo.It("converts rows and columns into data maps", func() {
		result := &api.ViewResult{
			Name:      "my-view",
			Namespace: "default",
			Title:     "My View",
			Columns: []view.ColumnDef{
				{Name: "dimension"},
				{Name: "value"},
			},
			Rows: []view.Row{
				{"Namespace", "dev"},
				{"Image", "nginx:latest"},
			},
		}
		sv := result.Serialized()
		Expect(sv.Name).To(Equal("my-view"))
		Expect(sv.Namespace).To(Equal("default"))
		Expect(sv.SerializedSection.Title).To(Equal("My View"))
		Expect(sv.SerializedSection.Data).To(HaveLen(2))
		Expect(sv.SerializedSection.Data[0]).To(Equal(map[string]any{"dimension": "Namespace", "value": "dev"}))
		Expect(sv.SerializedSection.Data[1]).To(Equal(map[string]any{"dimension": "Image", "value": "nginx:latest"}))
	})

	ginkgo.It("skips hidden and internal columns", func() {
		result := &api.ViewResult{
			Name: "v",
			Columns: []view.ColumnDef{
				{Name: "visible"},
				{Name: "hidden_col", Hidden: true},
				{Name: "attrs", Type: "row_attributes"},
				{Name: "grants_col", Type: "grants"},
			},
			Rows: []view.Row{{"val", "h", "a", "g"}},
		}
		sv := result.Serialized()
		Expect(sv.SerializedSection.Data).To(HaveLen(1))
		Expect(sv.SerializedSection.Data[0]).To(Equal(map[string]any{"visible": "val"}))
	})

	ginkgo.It("returns nil data when rows are empty", func() {
		result := &api.ViewResult{
			Name:    "empty",
			Columns: []view.ColumnDef{{Name: "col"}},
		}
		sv := result.Serialized()
		Expect(sv.SerializedSection.Data).To(BeNil())
	})

	ginkgo.It("converts sectionResults with view refs into sections", func() {
		child := &api.ViewResult{
			Name:    "child",
			Columns: []view.ColumnDef{{Name: "k"}},
			Rows:    []view.Row{{"v1"}},
		}
		parent := &api.ViewResult{
			Name: "parent",
			SectionResults: []api.ViewSectionResult{
				{Title: "Child Section", Icon: "box", View: child},
			},
		}
		sv := parent.Serialized()
		Expect(sv.Section).To(HaveLen(1))
		Expect(sv.Section[0].Title).To(Equal("Child Section"))
		Expect(sv.Section[0].Data).To(HaveLen(1))
		Expect(sv.Section[0].Data[0]).To(Equal(map[string]any{"k": "v1"}))
	})

	ginkgo.It("converts sectionResults with errors into sections with body", func() {
		result := &api.ViewResult{
			Name: "with-error",
			SectionResults: []api.ViewSectionResult{
				{Title: "Bad Section", Error: "failed to load"},
			},
		}
		sv := result.Serialized()
		Expect(sv.Section).To(HaveLen(1))
		Expect(sv.Section[0].Title).To(Equal("Bad Section"))
		Expect(sv.Section[0].Body.IsEmpty()).To(BeFalse())
	})
})

var _ = ginkgo.Describe("SerializedView MarshalJSON sections as map", func() {
	ginkgo.It("serializes sections as map keyed by lower_snake_case title", func() {
		v := api.SerializedView{
			Name: "my-view",
			Section: []api.SerializedSection{
				{Title: "Recent Changes", Data: []map[string]any{{"k": "v"}}},
				{Title: "Versions", Data: []map[string]any{{"a": "b"}}},
			},
		}
		b, err := json.Marshal(v)
		Expect(err).ToNot(HaveOccurred())

		var out map[string]any
		Expect(json.Unmarshal(b, &out)).To(Succeed())
		sections, ok := out["sections"].(map[string]any)
		Expect(ok).To(BeTrue(), "sections should be a map")
		Expect(sections).To(HaveKey("recent_changes"))
		Expect(sections).To(HaveKey("versions"))
		Expect(sections).ToNot(HaveKey("Recent Changes"))
	})
})

var _ = ginkgo.Describe("SerializedSection MarshalYAML", func() {
	ginkgo.It("omits Filters and Variables, keeps Title/Icon/Data", func() {
		s := api.SerializedSection{
			Title:     "Info",
			Icon:      "server",
			Data:      []map[string]any{{"key": "val"}},
			Variables: map[string]string{"x": "1"},
			Filters: map[string]api.ColumnFilterOptions{
				"col": {List: []string{"a"}},
			},
		}
		b, err := yaml.Marshal(s)
		Expect(err).ToNot(HaveOccurred())

		var out map[string]any
		Expect(yaml.Unmarshal(b, &out)).To(Succeed())
		Expect(out).To(HaveKey("title"))
		Expect(out).To(HaveKey("icon"))
		Expect(out).To(HaveKey("data"))
		Expect(out).ToNot(HaveKey("variables"))
		Expect(out).ToNot(HaveKey("filters"))
	})
})
