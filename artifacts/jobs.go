package artifacts

import (
	"fmt"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/artifacts/fs"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
)

// agentArtifactConnection is the cached agent artifact store connection
var agentArtifactConnection *models.Connection

func getArtifactStore(ctx context.Context) (fs.FilesystemRW, error) {
	if agentArtifactConnection == nil {
		artifactConnection, err := pkgConnection.Get(ctx, api.DefaultArtifactConnection)
		if err != nil {
			return nil, err
		} else if artifactConnection == nil {
			return nil, fmt.Errorf("artifact connection (%s) not found", api.DefaultArtifactConnection)
		}

		agentArtifactConnection = artifactConnection
	}

	agentArtifactStore, err := artifacts.GetFSForConnection(ctx, *agentArtifactConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact filesystem from connection: %w", err)
	} else if agentArtifactStore == nil {
		return nil, fmt.Errorf("a filesystem for the connection (%s) of type (%s) was not found", api.DefaultArtifactConnection, agentArtifactConnection.Type)
	}
	return agentArtifactStore, nil
}

// SyncArtifactItems pushes the artifact data.
func SyncArtifactItems(ctx context.Context, config upstream.UpstreamConfig, batchSize int) (int, error) {
	client := upstream.NewUpstreamClient(config)
	var count int
	var err error
	var fs fs.FilesystemRW
	for {
		var artifacts []models.Artifact
		if err := ctx.DB().Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("is_data_pushed IS FALSE").Where("is_pushed IS TRUE").Order("created_at").Limit(batchSize).Find(&artifacts).Error; err != nil {
			return 0, fmt.Errorf("failed to fetch artifacts: %w", err)
		}

		if len(artifacts) == 0 {
			return count, nil
		}

		for _, artifact := range artifacts {
			if fs == nil {
				fs, err = getArtifactStore(ctx)
				if err != nil {
					return 0, err
				}
			}
			reader, err := fs.Read(ctx, artifact.Path)
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
