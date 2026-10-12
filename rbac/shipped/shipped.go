// Package shipped writes the Roles and Scopes Mission Control ships: viewer and admin, in the mission-control
// namespace, with the Scopes they use (see specs/authorization/overview.md, "Shipped Roles").
package shipped

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/duty/context"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

//go:embed shipped.yaml
var manifests []byte

const (
	// RoleViewer and RoleAdmin are the names of the shipped Roles.
	RoleViewer = "viewer"
	RoleAdmin  = "admin"

	// allConfigs is the Scope admin's plugin rules are on.
	allConfigs = "all-configs"

	apiVersion = "mission-control.flanksource.com/v1"
)

// idNamespace derives the ids of shipped objects from their kind, namespace and name, so every Sync writes the same rows.
var idNamespace = uuid.MustParse("6f1d4c8e-5a0b-4e8f-9c3d-2b7a1e6f9d40")

// Objects returns the shipped Scopes and Roles, as written in shipped.yaml, without admin's plugin rules.
func Objects() ([]v1.Scope, []v1.Role, error) {
	var scopes []v1.Scope
	var roles []v1.Role

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(manifests)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("failed to read shipped.yaml: %w", err)
		} else if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		var typeMeta metav1.TypeMeta
		if err := yaml.Unmarshal(doc, &typeMeta); err != nil {
			return nil, nil, fmt.Errorf("failed to read shipped.yaml: %w", err)
		} else if typeMeta.APIVersion != apiVersion {
			return nil, nil, fmt.Errorf("shipped.yaml: unexpected apiVersion %q", typeMeta.APIVersion)
		}

		switch typeMeta.Kind {
		case "Scope":
			var scope v1.Scope
			if err := yaml.UnmarshalStrict(doc, &scope); err != nil {
				return nil, nil, fmt.Errorf("shipped.yaml: invalid scope: %w", err)
			}
			scopes = append(scopes, scope)
		case "Role":
			var role v1.Role
			if err := yaml.UnmarshalStrict(doc, &role); err != nil {
				return nil, nil, fmt.Errorf("shipped.yaml: invalid role: %w", err)
			}
			roles = append(roles, role)
		default:
			return nil, nil, fmt.Errorf("shipped.yaml: unexpected kind %q", typeMeta.Kind)
		}
	}

	return scopes, roles, nil
}

// Build returns the shipped Scopes and Roles, with one admin rule for each of the plugin operations, by plugin name.
// An operation whose action isn't valid, e.g. one with a space in its name, gets no rule.
func Build(operations map[string][]string) ([]v1.Scope, []v1.Role, error) {
	scopes, roles, err := Objects()
	if err != nil {
		return nil, nil, err
	}

	for i := range roles {
		if roles[i].Name == RoleAdmin {
			roles[i].Spec.Rules = append(roles[i].Spec.Rules, invokeRules(roles[i].Spec.Rules, operations)...)
		}
	}
	return scopes, roles, nil
}

// invokeRules returns one rule for each plugin operation, named so it doesn't clash with the existing rules.
func invokeRules(existing []v1.RoleRule, operations map[string][]string) []v1.RoleRule {
	used := lo.SliceToMap(existing, func(r v1.RoleRule) (string, bool) { return r.Name, true })

	var rules []v1.RoleRule
	plugins := lo.Keys(operations)
	slices.Sort(plugins)
	for _, name := range plugins {
		for _, op := range operations[name] {
			action := policy.NewPluginInvokeAction(name, op)
			if _, err := adapter.ContractFor(action); err != nil {
				continue
			}

			rule := v1.RoleRule{
				Name:     invokeRuleName(action, used),
				Action:   action,
				Resource: &v1.ScopeReference{ScopeRef: allConfigs},
			}
			used[rule.Name] = true
			rules = append(rules, rule)
		}
	}
	return rules
}

var notRuleNameChars = regexp.MustCompile(`[^a-z0-9]+`)

// invokeRuleName returns invoke-<plugin>-<operation>, or, when that isn't a valid rule name or is taken,
// a name ending in a hash of the action.
func invokeRuleName(action string, used map[string]bool) string {
	readable := strings.Trim(notRuleNameChars.ReplaceAllString(strings.ToLower(strings.TrimPrefix(action, policy.ActionPluginInvokePrefix)), "-"), "-")
	name := "invoke-" + readable
	if readable != "" && len(name) <= 63 && !used[name] {
		return name
	}

	sum := sha256.Sum256([]byte(action))
	return strings.TrimRight(name[:min(len(name), 54)], "-") + "-" + hex.EncodeToString(sum[:])[:8]
}

var syncLock sync.Mutex

// Sync writes the shipped Scopes and Roles, with admin's rules for the operations of the plugins installed now.
// It replaces any other Scope or Role of the same namespace and name, and deletes shipped ones that are no longer
// shipped, e.g. admin's rule for an operation that's gone. It's idempotent.
// A shipped object that's stored but not in effect is a bug, and is returned as an error.
func Sync(ctx context.Context) error {
	syncLock.Lock()
	defer syncLock.Unlock()

	scopes, roles, err := Build(plugin.DefaultRegistry.Operations())
	if err != nil {
		return err
	}

	var errs []error
	var scopeIDs, roleIDs []string
	for i := range scopes {
		scope := &scopes[i]
		scope.UID = objectID("scopes", scope.Namespace, scope.Name)
		scopeIDs = append(scopeIDs, string(scope.UID))
		if err := replaceOthers(ctx, "scopes", scope.ObjectMeta); err != nil {
			return err
		}
		if err := db.PersistScope(ctx, scope, adapter.SourceShipped, nil); err != nil {
			errs = append(errs, fmt.Errorf("scope %s/%s: %w", scope.Namespace, scope.Name, err))
		}
	}

	for i := range roles {
		role := &roles[i]
		role.UID = objectID("roles", role.Namespace, role.Name)
		roleIDs = append(roleIDs, string(role.UID))
		if err := replaceOthers(ctx, "roles", role.ObjectMeta); err != nil {
			return err
		}
		if err := db.PersistRole(ctx, role, adapter.SourceShipped, nil); err != nil {
			errs = append(errs, fmt.Errorf("role %s/%s: %w", role.Namespace, role.Name, err))
		}
	}

	if err := deleteUnshipped(ctx, "roles", roleIDs); err != nil {
		return err
	}
	if err := deleteUnshipped(ctx, "scopes", scopeIDs); err != nil {
		return err
	}

	if err := dutyRBAC.ReloadPolicy(); err != nil {
		errs = append(errs, fmt.Errorf("failed to reload policy: %w", err))
	}
	return errors.Join(errs...)
}

// Start syncs whenever the operations of the installed plugins change, until the context is done.
// Changes that arrive during a sync are synced once it's done.
func Start(ctx context.Context) {
	changed := make(chan struct{}, 1)
	plugin.DefaultRegistry.OnOperationsChanged(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-changed:
				if err := Sync(ctx); err != nil {
					ctx.Logger.Errorf("failed to sync shipped roles after plugin operations changed: %v", err)
				}
			}
		}
	}()
}

func objectID(table, namespace, name string) k8sTypes.UID {
	return k8sTypes.UID(uuid.NewSHA1(idNamespace, []byte(table+"/"+namespace+"/"+name)).String())
}

// replaceOthers deletes any other object in the table with the namespace and name, e.g. one applied through
// Kubernetes before Mission Control shipped one by that name, with a Scope's membership.
func replaceOthers(ctx context.Context, table string, meta metav1.ObjectMeta) error {
	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		var replaced []uuid.UUID
		if err := txCtx.DB().Raw(fmt.Sprintf(`UPDATE %s SET deleted_at = NOW()
			WHERE namespace = ? AND name = ? AND id != ? AND deleted_at IS NULL
			RETURNING id`, table), meta.Namespace, meta.Name, meta.UID).Scan(&replaced).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to replace %s %s/%s", table, meta.Namespace, meta.Name)
		}

		if table == "scopes" {
			for _, id := range replaced {
				if err := membership.Clear(txCtx, id); err != nil {
					return ctx.Oops().Wrap(err)
				}
			}
		}
		return nil
	})
}

// deleteUnshipped deletes the shipped objects in the table that aren't among the ids, with a Scope's membership.
func deleteUnshipped(ctx context.Context, table string, ids []string) error {
	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		var deleted []uuid.UUID
		if err := txCtx.DB().Raw(fmt.Sprintf(`UPDATE %s SET deleted_at = NOW()
			WHERE source = ? AND deleted_at IS NULL AND NOT (id::text = ANY (?))
			RETURNING id`, table), adapter.SourceShipped, pq.StringArray(ids)).Scan(&deleted).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to delete %s no longer shipped", table)
		}

		if table == "scopes" {
			for _, id := range deleted {
				if err := membership.Clear(txCtx, id); err != nil {
					return ctx.Oops().Wrap(err)
				}
			}
		}
		return nil
	})
}
