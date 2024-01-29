package artifacts

import (
	"errors"
	"fmt"
	"io"

	"github.com/flanksource/artifacts"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UploadArtifact Uploads the given artifact data to the default artifact store
func UploadArtifact(ctx context.Context, artifactID string, reader io.ReadCloser) error {
	if _, err := uuid.Parse(artifactID); err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, fmt.Sprintf("(%s) is not a valid uuid", artifactID))
	}

	var artifact models.Artifact
	if err := ctx.DB().Where("id = ?", artifactID).Find(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "artifact(%s) was not found", artifactID)
		}

		return fmt.Errorf("failed to get artifact: %w", err)
	}

	conn, err := ctx.HydrateConnectionByURL(api.DefaultArtifactConnection)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	} else if conn == nil {
		return dutyAPI.Errorf(dutyAPI.ENOTIMPLEMENTED, "the artifacts connection was not found")
	}

	fs, err := artifacts.GetFSForConnection(ctx, *conn)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer fs.Close()

	_, err = fs.Write(ctx, artifact.Path, reader)
	return err
}
