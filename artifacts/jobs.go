package artifacts

import (
	"fmt"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"gorm.io/gorm/clause"
)

// SyncArtifactItems pushes the artifact data.
func SyncArtifactItems(ctx context.Context, config upstream.UpstreamConfig, agentArtifactStore artifacts.FilesystemRW, batchSize int) (int, error) {
	client := upstream.NewUpstreamClient(config)
	var count int

	for {
		var artifacts []models.Artifact
		if err := ctx.DB().Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("is_data_pushed IS FALSE").Where("is_pushed IS TRUE").Order("created_at").Limit(batchSize).Find(&artifacts).Error; err != nil {
			return 0, fmt.Errorf("failed to fetch artifacts: %w", err)
		}

		if len(artifacts) == 0 {
			return count, nil
		}

		for _, artifact := range artifacts {
			reader, err := agentArtifactStore.Read(ctx, artifact.Path)
			if err != nil {
				return count, fmt.Errorf("failed to read remote artifact store: %w", err)
			}

			if err := client.PushArtifacts(ctx, artifact.ID, reader); err != nil {
				return count, fmt.Errorf("failed to push artifact (%s): %w", artifact.ID, err)
			}

			if err := ctx.DB().Model(&models.Artifact{}).Where("id = ?", artifact.ID).Update("is_data_pushed", true).Error; err != nil {
				return 0, fmt.Errorf("failed to update is_pushed on artifacts: %w", err)
			}

			count++
		}
	}
}
