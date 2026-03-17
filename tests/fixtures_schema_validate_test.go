package tests

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func validateFixtureDirWithSchema(schemaPath, dir string) {
	schema, err := jsonschema.NewCompiler().Compile(schemaPath)
	gomega.Expect(err).To(gomega.BeNil())

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			yamlRaw, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			var m any
			err = yaml.Unmarshal(yamlRaw, &m)
			if err != nil {
				return err
			}

			if err := schema.Validate(m); err != nil {
				return fmt.Errorf("schema validation failed for %s: %w", path, err)
			}
		}
		return nil
	})

	gomega.Expect(err).To(gomega.BeNil())
}

var _ = ginkgo.Describe("Fixture schema validation", func() {
	ginkgo.It("Notifications", func() {
		schemaPath := "../config/schemas/notification.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/notifications/")
	})

	ginkgo.It("NotificationSilence", func() {
		schemaPath := "../config/schemas/notificationsilence.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/silences/")
	})

	ginkgo.It("Playbooks", func() {
		schemaPath := "../config/schemas/playbook.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/playbooks/")
	})

	ginkgo.It("Rules", func() {
		schemaPath := "../config/schemas/incident-rules.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/rules")
	})
})
