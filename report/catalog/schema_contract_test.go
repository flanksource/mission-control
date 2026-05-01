package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type changeSchemaDocument struct {
	Defs map[string]changeSchemaDefinition `json:"$defs"`
}

type changeSchemaDefinition struct {
	Examples []json.RawMessage `json:"examples"`
}

func repoRelativePath(parts ...string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("failed to resolve test file path")
	}

	base := filepath.Dir(filename)
	allParts := append([]string{base}, parts...)
	return filepath.Join(allParts...)
}

func loadSchemaExampleKinds() ([]string, error) {
	schemaPath := repoRelativePath("..", "..", "..", "duty", "schema", "openapi", "change-types.schema.json")
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}

	var schema changeSchemaDocument
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}

	kinds := map[string]struct{}{}
	if definition, ok := schema.Defs["ConfigChangeDetailsSchema"]; ok {
		if err := addExampleKinds(kinds, definition.Examples); err != nil {
			return nil, err
		}
	}

	for name, definition := range schema.Defs {
		if name == "ConfigChangeDetailsSchema" {
			continue
		}
		if err := addExampleKinds(kinds, definition.Examples); err != nil {
			return nil, err
		}
	}

	return sortedKinds(kinds), nil
}

func addExampleKinds(kinds map[string]struct{}, examples []json.RawMessage) error {
	for _, example := range examples {
		var envelope struct {
			Kind string `json:"kind"`
		}

		if err := json.Unmarshal(example, &envelope); err != nil {
			return err
		}
		if envelope.Kind != "" {
			kinds[envelope.Kind] = struct{}{}
		}
	}

	return nil
}

func loadRendererKinds() ([]string, error) {
	rendererPath := repoRelativePath("..", "components", "change-section-utils.ts")
	raw, err := os.ReadFile(rendererPath)
	if err != nil {
		return nil, err
	}

	matches := regexp.MustCompile(`'([^']+/v1)'\s*:`).FindAllStringSubmatch(string(raw), -1)
	kinds := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			kinds[match[1]] = struct{}{}
		}
	}

	return sortedKinds(kinds), nil
}

func sortedKinds(kinds map[string]struct{}) []string {
	values := make([]string, 0, len(kinds))
	for kind := range kinds {
		values = append(values, kind)
	}
	sort.Strings(values)
	return values
}

func missingKinds(expected, actual []string) []string {
	actualSet := make(map[string]struct{}, len(actual))
	for _, kind := range actual {
		actualSet[kind] = struct{}{}
	}

	var missing []string
	for _, kind := range expected {
		if _, ok := actualSet[kind]; !ok {
			missing = append(missing, kind)
		}
	}

	return missing
}

var _ = ginkgo.Describe("Schema contract", func() {
	ginkgo.It("has a typed renderer for each standalone change schema example kind", func() {
		schemaKinds, err := loadSchemaExampleKinds()
		Expect(err).ToNot(HaveOccurred())

		rendererKinds, err := loadRendererKinds()
		Expect(err).ToNot(HaveOccurred())

		Expect(missingKinds(schemaKinds, rendererKinds)).To(BeEmpty())
	})
})
