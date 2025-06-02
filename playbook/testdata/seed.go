package testdata

import (
	"embed"
	"path/filepath"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed connections
var connectionDir embed.FS

//go:embed permissions
var permissionsDir embed.FS

func LoadPermissions(ctx context.Context) error {
	entries, err := permissionsDir.ReadDir("permissions")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		path := filepath.Join("permissions", entry.Name())
		content, err := permissionsDir.ReadFile(path)
		if err != nil {
			return err
		}

		var perm v1.Permission
		err = yamlutil.Unmarshal(content, &perm)
		if err != nil {
			return err
		}

		perm.UID = types.UID(uuid.New().String())

		err = db.PersistPermissionFromCRD(ctx, &perm)
		if err != nil {
			return err
		}
	}

	err = rbac.ReloadPolicy()
	if err != nil {
		return err
	}

	return nil
}

func LoadConnections(ctx context.Context) error {
	entries, err := connectionDir.ReadDir("connections")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		path := filepath.Join("connections", entry.Name())
		content, err := connectionDir.ReadFile(path)
		if err != nil {
			return err
		}

		var conn v1.Connection
		err = yamlutil.Unmarshal(content, &conn)
		if err != nil {
			return err
		}

		err = db.PersistConnectionFromCRD(ctx, &conn)
		if err != nil {
			return err
		}
	}

	return nil
}
