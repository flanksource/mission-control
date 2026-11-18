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
	"github.com/flanksource/duty/upstream"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/rbac"
)

const defaultMaxReadSize = 50 * 1024 * 1024 // 50 MB

type ArtifactContent struct {
	ActionID string `json:"action_id"`
	Path     string `json:"path"`
	Content  []byte `json:"content"`
}

// TODO: Memoize this
func GetArtifactFS(ctx context.Context) (fs.FilesystemRW, *models.Connection, error) {
	return getArtifactFS(ctx, pkgConnection.Get)
}

// dangerouslyGetArtifactFSBypassingAuthz returns the artifact store without checking any permission of
// ctx.Subject() on the artifact connection. Only use it where the server itself writes on behalf of a caller that
// can't be granted one, e.g. an agent uploading its own artifact's content: an agent can hold no Role
// (specs/authorization/identities.md, Section 5) and never sees the connection. Authorize the request some other way first.
func dangerouslyGetArtifactFSBypassingAuthz(ctx context.Context) (fs.FilesystemRW, *models.Connection, error) {
	return getArtifactFS(ctx, func(ctx context.Context, ref string) (*models.Connection, error) {
		return ctx.HydrateConnectionByURL(ref)
	})
}

// getArtifactFSForUse returns the artifact store to write or read a playbook run's artifacts with, as the playbook.
// It requires connection:use on the artifact connection, as ctx.Subject().
func getArtifactFSForUse(ctx context.Context) (fs.FilesystemRW, *models.Connection, error) {
	return getArtifactFS(ctx, rbac.GetConnectionForUse)
}

func getArtifactFS(ctx context.Context, getConnection func(context.Context, string) (*models.Connection, error)) (fs.FilesystemRW, *models.Connection, error) {
	if api.DefaultArtifactConnection == "" {
		return nil, nil, dutyAPI.Errorf(dutyAPI.ENOTIMPLEMENTED, "no artifact connection configured")
	}

	conn, err := getConnection(ctx, api.DefaultArtifactConnection)
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

	fs, connection, err := getArtifactFSForUse(ctx)
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

	fs, _, err := getArtifactFSForUse(ctx)
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
			Path:     a.Path,
			Content:  content,
		})
	}

	return contents, nil
}

// UploadArtifact uploads the content of the agent's artifact to the default artifact store.
func UploadArtifact(ctx context.Context, agentID uuid.UUID, artifactID string, reader io.ReadCloser) error {
	id, err := uuid.Parse(artifactID)
	if err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "(%s) is not a valid uuid", artifactID)
	}

	var artifact models.Artifact
	if err := ctx.DB().Where("id = ?", id).First(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "artifact(%s) was not found", artifactID)
		}

		return fmt.Errorf("failed to get artifact: %w", err)
	}

	if owned, err := upstream.IsOwnedBy(ctx, "artifacts", agentID, id); err != nil {
		return err
	} else if !owned {
		return dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "artifact(%s) belongs to another agent", artifactID)
	}

	fs, _, err := dangerouslyGetArtifactFSBypassingAuthz(ctx)
	if err != nil {
		return fmt.Errorf("failed to get artifact fs: %w", err)
	}
	defer fs.Close()

	_, err = fs.Write(ctx, artifact.Path, reader)
	return err
}
