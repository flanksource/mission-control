package testdata

import (
	"embed"
	"path/filepath"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed connections
var connectionDir embed.FS

//go:embed permissions
var permissionsDir embed.FS

//go:embed *.yaml
var playbookFiles embed.FS

func LoadPlaybooks(ctx context.Context) error {
	// These are created manually
	exclusions := map[string]bool{
		"agent-runner.yaml": true,
		"echo.yaml":         true,
	}

	entries, err := playbookFiles.ReadDir(".")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		if _, ok := exclusions[entry.Name()]; ok {
			continue
		}

		content, err := playbookFiles.ReadFile(entry.Name())
		if err != nil {
			return err
		}

		var typeMeta metav1.TypeMeta
		if err := yamlutil.Unmarshal(content, &typeMeta); err != nil {
			return err
		}

		if typeMeta.Kind != "Playbook" {
			continue
		}

		var playbook v1.Playbook
		if err := yamlutil.Unmarshal(content, &playbook); err != nil {
			return err
		}

		if playbook.Namespace == "" {
			playbook.Namespace = "default"
		}

		if err := db.PersistPlaybookFromCRD(ctx, &playbook); err != nil {
			return err
		}
	}

	return nil
}

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
