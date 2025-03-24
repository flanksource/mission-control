package runner

import (
	"fmt"
	"path/filepath"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/api"
)

func saveArtifacts(ctx context.Context, playbookActionID uuid.UUID, generatedArtifacts []artifacts.Artifact) error {
	if len(generatedArtifacts) == 0 {
		return nil
	}

	if api.DefaultArtifactConnection == "" {
		logger.Warnf("no artifact connection configured")
		return nil
	}

	connection, err := pkgConnection.Get(ctx, api.DefaultArtifactConnection)
	if err != nil {
		return fmt.Errorf("error getting connection(%s): %w", api.DefaultArtifactConnection, err)
	} else if connection == nil {
		return fmt.Errorf("connection(%s) was not found", api.DefaultArtifactConnection)
	}

	fs, err := artifacts.GetFSForConnection(ctx, *connection)
	if err != nil {
		return fmt.Errorf("error getting filesystem for connection: %w", err)
	}
	defer fs.Close()

	for _, a := range generatedArtifacts {
		a.Path = filepath.Join("playbooks", playbookActionID.String(), a.Path)
		artifact := models.Artifact{
			PlaybookRunActionID: utils.Ptr(playbookActionID),
			ConnectionID:        connection.ID,
		}

		if err := artifacts.SaveArtifact(ctx, fs, &artifact, a); err != nil {
			return fmt.Errorf("error saving artifact to db: %w", err)
		}
	}

	return nil
}
