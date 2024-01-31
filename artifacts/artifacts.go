package artifacts

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func init() {
	if api.ArtifactsTempDir != "" {
		if err := os.MkdirAll(api.ArtifactsTempDir, os.ModePerm); err != nil {
			logger.Fatalf("failed to create artifacts cache dir %s: %v", api.ArtifactsTempDir, err)
		}
	}
}

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

	var artifactType = "playbooks"
	if artifact.CheckID != nil {
		artifactType = "checks"
	}

	downloadPath := filepath.Join(api.ArtifactsTempDir, fmt.Sprintf("%s-%s.zip", artifactType, artifactID))
	f, err := os.Create(downloadPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", downloadPath, err)
	}
	defer f.Close()

	b, err := io.Copy(f, reader)
	if err != nil {
		return fmt.Errorf("failed to download to %s: %w", downloadPath, err)
	}

	zipReader, err := zip.NewReader(f, b)
	if err != nil {
		return fmt.Errorf("failed to create a zip reader %s: %w", downloadPath, err)
	}

	baseDir := filepath.Join(artifactType, artifactID)
	for _, zipFile := range zipReader.File {
		if zipFile.FileInfo().IsDir() {
			logger.Infof("Skipping directory %s in zip", zipFile.Name)
			continue
		}

		err := uploadToArtifact(ctx, baseDir, fs, zipFile)
		if err != nil {
			return fmt.Errorf("failed to upload %s: %w", zipFile.Name, err)
		}
	}

	return nil
}

func uploadToArtifact(ctx context.Context, prefix string, fs artifacts.FilesystemRW, zipFile *zip.File) error {
	f, err := zipFile.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fs.Write(ctx, filepath.Join(prefix, zipFile.Name), f)
	return err
}
