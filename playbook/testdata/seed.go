package testdata

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/flanksource/duty/context"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed connections
var connectionDir embed.FS

//go:embed rbac
var rbacDir embed.FS

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

// LoadRBAC persists the Scopes, Roles and RoleBindings of the rbac directory, and reloads the policy.
func LoadRBAC(ctx context.Context) error {
	entries, err := rbacDir.ReadDir("rbac")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		content, err := rbacDir.ReadFile(filepath.Join("rbac", entry.Name()))
		if err != nil {
			return err
		}

		if err := PersistRBAC(ctx, content); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
	}

	return adapter.ReloadPolicy()
}

// PersistRBAC persists the Scopes, Roles and RoleBindings of a multi-document YAML, in that order.
// Objects without a namespace go in the default namespace. Objects without a UID get one derived from their kind,
// namespace and name, so persisting them again updates them.
func PersistRBAC(ctx context.Context, content []byte) error {
	var scopes []v1.Scope
	var roles []v1.Role
	var bindings []v1.RoleBinding

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		var typeMeta metav1.TypeMeta
		if err := json.Unmarshal(raw, &typeMeta); err != nil {
			return err
		}

		var target any
		switch typeMeta.Kind {
		case "Scope":
			scopes = append(scopes, v1.Scope{})
			target = &scopes[len(scopes)-1]
		case "Role":
			roles = append(roles, v1.Role{})
			target = &roles[len(roles)-1]
		case "RoleBinding":
			bindings = append(bindings, v1.RoleBinding{})
			target = &bindings[len(bindings)-1]
		default:
			return fmt.Errorf("unsupported kind %q", typeMeta.Kind)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return err
		}
	}

	for i := range scopes {
		setMeta("Scope", &scopes[i].ObjectMeta)
		if err := db.PersistScopeFromCRD(ctx, &scopes[i]); err != nil {
			return err
		}
	}
	for i := range roles {
		setMeta("Role", &roles[i].ObjectMeta)
		if err := db.PersistRoleFromCRD(ctx, &roles[i]); err != nil {
			return err
		}
	}
	for i := range bindings {
		setMeta("RoleBinding", &bindings[i].ObjectMeta)
		if err := db.PersistRoleBindingFromCRD(ctx, &bindings[i]); err != nil {
			return err
		}
	}

	return nil
}

func setMeta(kind string, meta *metav1.ObjectMeta) {
	if meta.Namespace == "" {
		meta.Namespace = "default"
	}
	if meta.UID == "" {
		meta.UID = types.UID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(kind+"/"+meta.Namespace+"/"+meta.Name)).String())
	}
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
