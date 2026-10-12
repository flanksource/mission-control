// Package shipped writes the Roles and Scopes Mission Control ships: viewer and admin, in the
// mission-control-builtin namespace, with the Scopes they use (see specs/authorization/overview.md, "Shipped Roles").
package shipped

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/kopper"
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

// Sync writes the shipped Scopes and Roles, with admin's rules for the operations of the installed plugins.
// It replaces any other Scope or Role of the same namespace and name, and deletes shipped ones that are no longer
// shipped, e.g. admin's rule for an operation that's gone. It's idempotent, and writes everything or nothing.
// The policy is reloaded whenever Sync may have changed something, even when it fails.
// A shipped object that's stored but not in effect is a bug, and is returned as an error.
func Sync(ctx context.Context) error {
	syncLock.Lock()
	defer syncLock.Unlock()

	var notReady []error
	err := ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		operations, err := installedOperations(txCtx)
		if err != nil {
			return err
		}

		scopes, roles, err := Build(operations)
		if err != nil {
			return err
		}

		var scopeIDs, roleIDs []string
		for i := range scopes {
			scope := &scopes[i]
			scope.UID = objectID("scopes", scope.Namespace, scope.Name)
			scopeIDs = append(scopeIDs, string(scope.UID))
			if err := replaceOthers(txCtx, "scopes", scope.ObjectMeta); err != nil {
				return err
			}
			if err := persisted(db.PersistScope(txCtx, scope, adapter.SourceShipped, nil), &notReady); err != nil {
				return fmt.Errorf("scope %s/%s: %w", scope.Namespace, scope.Name, err)
			}
		}

		for i := range roles {
			role := &roles[i]
			role.UID = objectID("roles", role.Namespace, role.Name)
			roleIDs = append(roleIDs, string(role.UID))
			if err := replaceOthers(txCtx, "roles", role.ObjectMeta); err != nil {
				return err
			}
			if err := persisted(db.PersistRole(txCtx, role, adapter.SourceShipped, nil), &notReady); err != nil {
				return fmt.Errorf("role %s/%s: %w", role.Namespace, role.Name, err)
			}
		}

		if err := deleteUnshipped(txCtx, "roles", roleIDs); err != nil {
			return err
		}
		return deleteUnshipped(txCtx, "scopes", scopeIDs)
	})

	errs := append([]error{err}, notReady...)
	if err := dutyRBAC.ReloadPolicy(); err != nil {
		errs = append(errs, fmt.Errorf("failed to reload policy: %w", err))
	}
	return errors.Join(errs...)
}

// persisted returns the error of a persist that failed, and adds one that stored an object that isn't in effect
// to notReady instead, so the object is still written.
func persisted(err error, notReady *[]error) error {
	var notReadyErr *kopper.NotReadyError
	if errors.As(err, &notReadyErr) {
		*notReady = append(*notReady, err)
		return nil
	}
	return err
}

// installedOperations returns the operations admin gets rules for: those of the plugins that have registered, and,
// for installed plugins that haven't registered yet, e.g. right after a restart, the operations admin already has
// rules for. So a restart doesn't take plugin operations away from admins until each plugin is back.
// A plugin is installed when it's stored or in the registry; plugins proxied by agents are only in the registry.
func installedOperations(ctx context.Context) (map[string][]string, error) {
	operations := plugin.DefaultRegistry.Operations()

	pending := map[string]bool{}
	for _, entry := range plugin.DefaultRegistry.List() {
		pending[entry.Name] = true
	}
	rows, err := db.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		pending[row.Name] = true
	}
	for name := range operations {
		delete(pending, name)
	}
	if len(pending) == 0 {
		return operations, nil
	}

	var stored []models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND source = ? AND deleted_at IS NULL",
		adapter.ShippedNamespace, RoleAdmin, adapter.SourceShipped).Find(&stored).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	for _, role := range stored {
		var rules []v1.RoleRule
		if err := json.Unmarshal(role.Rules, &rules); err != nil {
			return nil, ctx.Oops().Wrapf(err, "invalid rules of role %s/%s", role.Namespace, role.Name)
		}
		for _, rule := range rules {
			rest, ok := strings.CutPrefix(rule.Action, policy.ActionPluginInvokePrefix)
			if !ok {
				continue
			}
			if name, op, ok := strings.Cut(rest, ":"); ok && pending[name] {
				operations[name] = append(operations[name], op)
			}
		}
	}

	for name, ops := range operations {
		slices.Sort(ops)
		operations[name] = slices.Compact(ops)
	}
	return operations, nil
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
// Kubernetes before Mission Control shipped one by that name.
func replaceOthers(ctx context.Context, table string, meta metav1.ObjectMeta) error {
	return softDelete(ctx, table, "namespace = ? AND name = ? AND id != ?", meta.Namespace, meta.Name, meta.UID)
}

// deleteUnshipped deletes the shipped objects in the table that aren't among the ids.
func deleteUnshipped(ctx context.Context, table string, ids []string) error {
	return softDelete(ctx, table, "source = ? AND NOT (id::text = ANY (?))", adapter.SourceShipped, pq.StringArray(ids))
}

// softDelete soft deletes the objects in the table that match the condition, and clears a Scope's membership.
func softDelete(ctx context.Context, table, condition string, args ...any) error {
	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		var deleted []uuid.UUID
		if err := txCtx.DB().Raw(fmt.Sprintf(`UPDATE %s SET deleted_at = NOW() WHERE deleted_at IS NULL AND %s RETURNING id`, table, condition),
			args...).Scan(&deleted).Error; err != nil {
			return ctx.Oops().Wrapf(err, "failed to delete %s", table)
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
