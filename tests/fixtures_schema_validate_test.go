package tests

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func TestFixtures(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fixture schema validation")
}

func validateFixtureDirWithSchema(schema *jsonschema.Schema, dir string) {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			yamlRaw, err := os.ReadFile(path)
			Expect(err).To(BeNil())

			var m any
			err = yaml.Unmarshal(yamlRaw, &m)
			Expect(err).To(BeNil())

			err = schema.Validate(m)
			if err != nil {
				err = fmt.Errorf("schema validation failed for %s: %w", path, err)
			}
			Expect(err).To(BeNil())
		}
		return nil
	})
	Expect(err).To(BeNil())
}

var _ = Describe("Fixture schema validation", func() {
	It("Notifications", func() {
		schemaPath := "../config/schemas/notification.schema.json"
		c := jsonschema.NewCompiler()
		schema, err := c.Compile(schemaPath)
		Expect(err).To(BeNil())
		validateFixtureDirWithSchema(schema, "../fixtures/notifications/")
	})

	It("Playbooks", func() {
		schemaPath := "../config/schemas/playbook.schema.json"
		c := jsonschema.NewCompiler()
		schema, err := c.Compile(schemaPath)
		Expect(err).To(BeNil())
		validateFixtureDirWithSchema(schema, "../fixtures/playbooks/")
	})

	It("Rules", func() {
		schemaPath := "../config/schemas/incident-rules.schema.json"
		c := jsonschema.NewCompiler()
		schema, err := c.Compile(schemaPath)
		Expect(err).To(BeNil())
		validateFixtureDirWithSchema(schema, "../fixtures/rules")
	})
})
