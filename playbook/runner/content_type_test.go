package runner

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ParseContentEnvelope", func() {
	tests := []struct {
		name        string
		input       string
		wantContent string
		wantCT      string
	}{
		{
			name:        "valid envelope",
			input:       `{"content": "# Hello", "contentType": "text/markdown"}`,
			wantContent: "# Hello",
			wantCT:      "text/markdown",
		},
		{
			name:  "missing content field",
			input: `{"contentType": "text/markdown"}`,
		},
		{
			name:  "missing contentType field",
			input: `{"content": "hello"}`,
		},
		{
			name:  "not JSON",
			input: "just plain text",
		},
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "invalid JSON",
			input: `{"content": broken}`,
		},
		{
			name:  "JSON array",
			input: `[1, 2, 3]`,
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			content, ct := parseContentEnvelope(tt.input)
			Expect(content).To(Equal(tt.wantContent))
			Expect(ct).To(Equal(tt.wantCT))
		})
	}
})

var _ = ginkgo.Describe("ExtractContentType", func() {
	ginkgo.It("exec with JSON envelope in stdout", func() {
		data := map[string]any{
			"stdout": `{"content": "# Report", "contentType": "text/markdown"}`,
			"stderr": "",
		}
		result := extractContentType(data, "exec", "").(map[string]any)
		Expect(result["stdout"]).To(Equal("# Report"))
		Expect(result["contentType"]).To(Equal("text/markdown"))
	})

	ginkgo.It("exec with plain stdout", func() {
		data := map[string]any{
			"stdout": "hello world",
		}
		result := extractContentType(data, "exec", "").(map[string]any)
		Expect(result["stdout"]).To(Equal("hello world"))
		Expect(result).NotTo(HaveKey("contentType"))
	})

	ginkgo.It("spec-level override", func() {
		data := map[string]any{
			"stdout": `{"content": "# Report", "contentType": "text/markdown"}`,
		}
		result := extractContentType(data, "exec", "application/json").(map[string]any)
		Expect(result["stdout"]).To(Equal("# Report"))
		Expect(result["contentType"]).To(Equal("application/json"))
	})

	ginkgo.It("http with Content-Type header", func() {
		data := map[string]any{
			"content": "some body",
			"headers": map[string]any{"Content-Type": "text/markdown"},
		}
		result := extractContentType(data, "http", "").(map[string]any)
		Expect(result["contentType"]).To(Equal("text/markdown"))
	})

	ginkgo.It("http envelope overrides header", func() {
		data := map[string]any{
			"content": `{"content": "body", "contentType": "application/json"}`,
			"headers": map[string]any{"Content-Type": "text/plain"},
		}
		result := extractContentType(data, "http", "").(map[string]any)
		Expect(result["content"]).To(Equal("body"))
		Expect(result["contentType"]).To(Equal("application/json"))
	})

	ginkgo.It("nil data with spec contentType", func() {
		result := extractContentType(nil, "exec", "text/plain").(map[string]any)
		Expect(result["contentType"]).To(Equal("text/plain"))
	})

	ginkgo.It("nil data without spec contentType", func() {
		Expect(extractContentType(nil, "exec", "")).To(BeNil())
	})

	ginkgo.It("sql action type no primary key", func() {
		data := map[string]any{"rows": []any{}}
		result := extractContentType(data, "sql", "").(map[string]any)
		Expect(result).NotTo(HaveKey("contentType"))
	})

	ginkgo.It("spec contentType on sql action", func() {
		data := map[string]any{"rows": []any{}}
		result := extractContentType(data, "sql", "text/markdown").(map[string]any)
		Expect(result["contentType"]).To(Equal("text/markdown"))
	})
})
