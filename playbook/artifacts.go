package playbook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/artifacts"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

const maxBytesForMimeDetection = 512 * 1024 // 512KB

func saveArtifacts(ctx context.Context, playbookRunID uuid.UUID, generatedArtifacts []actions.ArtifactResult) error {
	if api.DefaultArtifactConnection == "" || len(generatedArtifacts) == 0 {
		return nil
	}

	connection, err := ctx.HydrateConnectionByURL(api.DefaultArtifactConnection)
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
		defer a.Content.Close()

		checksum := sha256.New()
		clonedReader := io.TeeReader(a.Content, checksum)

		mr := &mimeWriter{max: maxBytesForMimeDetection}
		clonedReader2 := io.TeeReader(clonedReader, mr)

		a.Path = filepath.Join("playbooks", playbookRunID.String(), a.Path)
		info, err := fs.Write(ctx, a.Path, clonedReader2)
		if err != nil {
			logger.Errorf("error saving artifact to filestore: %v", err)
			continue
		}

		if a.ContentType == "" {
			a.ContentType = mr.Detect().String()
		}

		artifact := models.Artifact{
			PlaybookRunID: utils.Ptr(playbookRunID),
			ConnectionID:  connection.ID,
			Path:          a.Path,
			Filename:      info.Name(),
			Size:          info.Size(),
			ContentType:   a.ContentType,
			Checksum:      hex.EncodeToString(checksum.Sum(nil)),
		}

		if err := ctx.DB().Create(&artifact).Error; err != nil {
			logger.Errorf("error saving artifact to db: %v", err)
		}
	}

	return nil
}

// mimeWriter implements io.Writer with a limit on the number of bytes used for detection.
type mimeWriter struct {
	buffer []byte
	max    int // max number of bytes to use from the source
}

func (t *mimeWriter) Write(bb []byte) (n int, err error) {
	if len(t.buffer) > t.max {
		return 0, nil
	}

	rem := t.max - len(t.buffer)
	if rem > len(bb) {
		rem = len(bb)
	}
	t.buffer = append(t.buffer, bb[:rem]...)
	return rem, nil
}

func (t *mimeWriter) Detect() *mimetype.MIME {
	return mimetype.Detect(t.buffer)
}
