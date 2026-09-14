package tests

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func validateFixtureDirWithSchema(schemaPath, dir, kind string) {
	schema, err := jsonschema.NewCompiler().Compile(schemaPath)
	gomega.Expect(err).To(gomega.BeNil())

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		decoder := yaml.NewDecoder(f)
		for {
			var doc any
			if err := decoder.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}

				return fmt.Errorf("failed to decode %s: %w", path, err)
			}

			m, ok := doc.(map[string]any)
			if !ok || m["kind"] != kind {
				continue
			}

			if err := schema.Validate(doc); err != nil {
				return fmt.Errorf("schema validation failed for %s: %w", path, err)
			}
		}
	})

	gomega.Expect(err).To(gomega.BeNil())
}

var _ = ginkgo.Describe("Fixture schema validation", func() {
	ginkgo.It("Notifications", func() {
		schemaPath := "../config/schemas/notification.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/notifications/", "Notification")
	})

	ginkgo.It("NotificationSilence", func() {
		schemaPath := "../config/schemas/notificationsilence.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/silences/", "NotificationSilence")
	})

	ginkgo.It("Playbooks", func() {
		schemaPath := "../config/schemas/playbook.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/playbooks/", "Playbook")
	})

	ginkgo.It("Rules", func() {
		schemaPath := "../config/schemas/incident-rules.schema.json"
		validateFixtureDirWithSchema(schemaPath, "../fixtures/rules", "IncidentRule")
	})
})
