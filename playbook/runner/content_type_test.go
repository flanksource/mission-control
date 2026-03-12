package runner

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestParseContentEnvelope(t *testing.T) {
	g := gomega.NewWithT(t)

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
		t.Run(tt.name, func(t *testing.T) {
			content, ct := parseContentEnvelope(tt.input)
			g.Expect(content).To(gomega.Equal(tt.wantContent))
			g.Expect(ct).To(gomega.Equal(tt.wantCT))
		})
	}
}

func TestExtractContentType(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("exec with JSON envelope in stdout", func(t *testing.T) {
		data := map[string]any{
			"stdout": `{"content": "# Report", "contentType": "text/markdown"}`,
			"stderr": "",
		}
		result := extractContentType(data, "exec", "").(map[string]any)
		g.Expect(result["stdout"]).To(gomega.Equal("# Report"))
		g.Expect(result["contentType"]).To(gomega.Equal("text/markdown"))
	})

	t.Run("exec with plain stdout", func(t *testing.T) {
		data := map[string]any{
			"stdout": "hello world",
		}
		result := extractContentType(data, "exec", "").(map[string]any)
		g.Expect(result["stdout"]).To(gomega.Equal("hello world"))
		g.Expect(result).NotTo(gomega.HaveKey("contentType"))
	})

	t.Run("spec-level override", func(t *testing.T) {
		data := map[string]any{
			"stdout": `{"content": "# Report", "contentType": "text/markdown"}`,
		}
		result := extractContentType(data, "exec", "application/json").(map[string]any)
		g.Expect(result["stdout"]).To(gomega.Equal("# Report"))
		g.Expect(result["contentType"]).To(gomega.Equal("application/json"))
	})

	t.Run("http with Content-Type header", func(t *testing.T) {
		data := map[string]any{
			"content": "some body",
			"headers": map[string]any{"Content-Type": "text/markdown"},
		}
		result := extractContentType(data, "http", "").(map[string]any)
		g.Expect(result["contentType"]).To(gomega.Equal("text/markdown"))
	})

	t.Run("http envelope overrides header", func(t *testing.T) {
		data := map[string]any{
			"content": `{"content": "body", "contentType": "application/json"}`,
			"headers": map[string]any{"Content-Type": "text/plain"},
		}
		result := extractContentType(data, "http", "").(map[string]any)
		g.Expect(result["content"]).To(gomega.Equal("body"))
		g.Expect(result["contentType"]).To(gomega.Equal("application/json"))
	})

	t.Run("nil data with spec contentType", func(t *testing.T) {
		result := extractContentType(nil, "exec", "text/plain").(map[string]any)
		g.Expect(result["contentType"]).To(gomega.Equal("text/plain"))
	})

	t.Run("nil data without spec contentType", func(t *testing.T) {
		g.Expect(extractContentType(nil, "exec", "")).To(gomega.BeNil())
	})

	t.Run("sql action type no primary key", func(t *testing.T) {
		data := map[string]any{"rows": []any{}}
		result := extractContentType(data, "sql", "").(map[string]any)
		g.Expect(result).NotTo(gomega.HaveKey("contentType"))
	})

	t.Run("spec contentType on sql action", func(t *testing.T) {
		data := map[string]any{"rows": []any{}}
		result := extractContentType(data, "sql", "text/markdown").(map[string]any)
		g.Expect(result["contentType"]).To(gomega.Equal("text/markdown"))
	})
}
