package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/utils"
	"github.com/spf13/cobra"
	"gorm.io/gorm/clause"
)

var Sync = &cobra.Command{
	Use:    "sync",
	PreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		for _, file := range args {
			data, err := readFile(file)
			if err != nil {
				logger.Fatalf("Failed to read file %s: %v", file, err)
			}
			objects, err := utils.GetUnstructuredObjects(data)
			if err != nil {
				logger.Fatalf("Failed to parse file %s: %v", file, err)
			}
			for _, object := range objects {
				if object.GetKind() == "IncidentRule" {
					rule := models.IncidentRule{
						Name:   object.GetName(),
						Source: "file://" + strings.ReplaceAll(file, cwd, ""),
					}

					spec, err := json.MarshalIndent(object.Object["spec"], "", "  ")
					if err != nil {
						logger.Fatalf("Failed to marshal spec: %v", err)
					}
					rule.Spec = types.JSON(spec)

					tx := db.Gorm.Table("incident_rules").Clauses(clause.OnConflict{
						Columns:   []clause.Column{{Name: "name"}},
						UpdateAll: true,
					}).Create(&rule)

					if tx.Error != nil {
						logger.Fatalf("Failed to create rule: %v", tx.Error)
					}
					if tx.RowsAffected > 0 {
						logger.Infof("Synced rule %s (%s)", rule.Name, rule.ID)
					}
				}
				// if object.GetKind() == "SystemTemplate" {
				// }
			}

		}
	},
}

func readFile(path string) ([]byte, error) {
	var data []byte
	var err error
	if path == "-" {
		if data, err = io.ReadAll(os.Stdin); err != nil {
			return nil, err
		}
	} else {
		if data, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	}
	return data, nil
}
