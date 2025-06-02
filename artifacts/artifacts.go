package artifacts

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/artifacts/fs"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
	pkgConnection "github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
)

const defaultMaxReadSize = 50 * 1024 * 1024 // 50 MB

type ArtifactContent struct {
	ActionID string `json:"action_id"`
	Content  []byte `json:"content"`
}

// TODO: Memoize this
func GetArtifactFS(ctx context.Context) (fs.FilesystemRW, *models.Connection, error) {
	if api.DefaultArtifactConnection == "" {
		return nil, nil, dutyAPI.Errorf(dutyAPI.ENOTIMPLEMENTED, "no artifact connection configured")
	}

	conn, err := pkgConnection.Get(ctx, api.DefaultArtifactConnection)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get connection: %w", err)
	} else if conn == nil {
		return nil, nil, dutyAPI.Errorf(dutyAPI.ENOTIMPLEMENTED, "the artifacts connection was not found")
	}

	fs, err := artifacts.GetFSForConnection(ctx, *conn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get connection: %w", err)
	}

	return fs, conn, nil
}

func SaveArtifacts(ctx context.Context, playbookActionID uuid.UUID, generatedArtifacts []artifacts.Artifact) error {
	if len(generatedArtifacts) == 0 {
		return nil
	}

	if api.DefaultArtifactConnection == "" {
		logger.Warnf("no artifact connection configured")
		return nil
	}

	fs, connection, err := GetArtifactFS(ctx)
	if err != nil {
		return fmt.Errorf("failed to get artifact fs: %w", err)
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

func GetArtifactContents(ctx context.Context, actionIDs ...string) ([]ArtifactContent, error) {
	if len(actionIDs) == 0 {
		return nil, nil
	}

	if api.DefaultArtifactConnection == "" {
		logger.Warnf("no artifact connection configured")
		return nil, nil
	}

	var actionArtifacts []models.Artifact
	if err := ctx.DB().Where("playbook_run_action_id IN ?", actionIDs).Find(&actionArtifacts).Error; err != nil {
		return nil, fmt.Errorf("failed to get playbook run action artifacts: %w", err)
	}

	fs, _, err := GetArtifactFS(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact fs: %w", err)
	}
	defer fs.Close()

	var contents []ArtifactContent
	for _, a := range actionArtifacts {
		reader, err := fs.Read(ctx, a.Path)
		if err != nil {
			return nil, fmt.Errorf("error loading artifact: %w", err)
		}
		defer reader.Close()

		if maxSize := int64(ctx.Properties().Int("artifacts.max_read_size", defaultMaxReadSize)); maxSize > 0 {
			// Safeguard for rare cases.
			reader = io.NopCloser(io.LimitReader(reader, maxSize))
		}

		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("error reading artifact: %w", err)
		}

		contents = append(contents, ArtifactContent{
			ActionID: a.PlaybookRunActionID.String(),
			Content:  content,
		})
	}

	return contents, nil
}

// UploadArtifact Uploads the given artifact data to the default artifact store
func UploadArtifact(ctx context.Context, artifactID string, reader io.ReadCloser) error {
	if _, err := uuid.Parse(artifactID); err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "(%s) is not a valid uuid", artifactID)
	}

	var artifact models.Artifact
	if err := ctx.DB().Where("id = ?", artifactID).Find(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "artifact(%s) was not found", artifactID)
		}

		return fmt.Errorf("failed to get artifact: %w", err)
	}

	fs, _, err := GetArtifactFS(ctx)
	if err != nil {
		return fmt.Errorf("failed to get artifact fs: %w", err)
	}
	defer fs.Close()

	_, err = fs.Write(ctx, artifact.Path, reader)
	return err
}
