package main

import (
	"os"
	"path"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/schema/openapi"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/spf13/cobra"
)

var schemas = map[string]any{
	"connection":     &v1.Connection{},
	"notification":   &v1.Notification{},
	"playbook":       &v1.Playbook{},
	"playbook-spec":  &v1.PlaybookSpec{}, // for go-side validation
	"incident-rules": &v1.IncidentRule{},
}

var generateSchema = &cobra.Command{
	Use: "generate-schema",
	Run: func(cmd *cobra.Command, args []string) {
		_ = os.Mkdir(schemaPath, 0755)
		for file, obj := range schemas {
			p := path.Join(schemaPath, file+".schema.json")
			if err := openapi.WriteSchemaToFile(p, obj); err != nil {
				logger.Fatalf("unable to save schema: %v", err)
			}
			logger.Infof("Saved OpenAPI schema to %s", p)
		}
	},
}

var schemaPath string

func main() {
	generateSchema.Flags().StringVar(&schemaPath, "schema-path", "../../config/schemas", "Path to save JSON schema to")
	if err := generateSchema.Execute(); err != nil {
		os.Exit(1)
	}
}
