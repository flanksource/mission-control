// Package shipped writes the objects Mission Control ships: the viewer, admin and plugin-admin Roles, in the
// mission-control-builtin namespace, with the Scopes they use, and the admins team bound to admin and plugin-admin
// (see specs/authorization/overview.md, "Shipped Roles" and "Configured bindings").
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
	// RoleViewer and RoleAdmin are the names of the shipped Roles written in shipped.yaml.
	RoleViewer = "viewer"
	RoleAdmin  = "admin"

	// RolePluginAdmin is the shipped Role that allows every operation of the installed plugins. It's built from
	// them, apart from admin, so a plugin can only ever change or invalidate this Role.
	RolePluginAdmin = adapter.RolePluginAdmin

	// TeamAdmins is the shipped team. BindingAdmins binds admin to it, and BindingPluginAdmins plugin-admin.
	TeamAdmins          = "admins"
	BindingAdmins       = "admins"
	BindingPluginAdmins = adapter.BindingPluginAdmins

	// allConfigs is the Scope plugin-admin's rules are on.
	allConfigs = "all-configs"

	apiVersion = "mission-control.flanksource.com/v1"
)

// idNamespace derives the ids of shipped objects from their kind, namespace and name, so every Sync writes the same rows.
var idNamespace = uuid.MustParse("6f1d4c8e-5a0b-4e8f-9c3d-2b7a1e6f9d40")

// Objects are the shipped objects, of each kind.
type Objects struct {
	Scopes       []v1.Scope
	Roles        []v1.Role
	Teams        []v1.Team
	RoleBindings []v1.RoleBinding
}

// Load returns the shipped objects as written in shipped.yaml, i.e. without plugin-admin.
func Load() (Objects, error) {
	var objects Objects

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(manifests)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Objects{}, fmt.Errorf("failed to read shipped.yaml: %w", err)
		} else if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		var typeMeta metav1.TypeMeta
		if err := yaml.Unmarshal(doc, &typeMeta); err != nil {
			return Objects{}, fmt.Errorf("failed to read shipped.yaml: %w", err)
		} else if typeMeta.APIVersion != apiVersion {
			return Objects{}, fmt.Errorf("shipped.yaml: unexpected apiVersion %q", typeMeta.APIVersion)
		}

		switch typeMeta.Kind {
		case "Scope":
			err = decode(doc, &objects.Scopes)
		case "Role":
			err = decode(doc, &objects.Roles)
		case "Team":
			err = decode(doc, &objects.Teams)
		case "RoleBinding":
			err = decode(doc, &objects.RoleBindings)
		default:
			err = fmt.Errorf("unexpected kind %q", typeMeta.Kind)
		}
		if err != nil {
			return Objects{}, fmt.Errorf("shipped.yaml: %w", err)
		}
	}

	return objects, nil
}

// decode strictly decodes a document and appends it to the list.
func decode[T any](doc []byte, list *[]T) error {
	var obj T
	if err := yaml.UnmarshalStrict(doc, &obj); err != nil {
		return err
	}
	*list = append(*list, obj)
	return nil
}

// Build returns the shipped objects, and plugin-admin with one rule for each of the plugin operations, by plugin
// name. An operation whose action isn't valid, e.g. one with a space in its name, gets no rule.
// Without any operation there's no plugin-admin, since a Role needs a rule, nor any binding to it.
func Build(operations map[string][]string) (Objects, error) {
	objects, err := Load()
	if err != nil {
		return Objects{}, err
	}

	if rules := invokeRules(operations); len(rules) > 0 {
		objects.Roles = append(objects.Roles, v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: RolePluginAdmin, Namespace: adapter.ShippedNamespace},
			Spec: v1.RoleSpec{
				Description: "Invokes every operation of the installed plugins, on any config",
				Rules:       rules,
			},
		})
	}

	roles := lo.SliceToMap(objects.Roles, func(r v1.Role) (string, bool) { return r.Name, true })
	objects.RoleBindings = lo.Filter(objects.RoleBindings, func(b v1.RoleBinding, _ int) bool { return roles[b.Spec.Role] })
	return objects, nil
}

// invokeRules returns one rule for each plugin operation, each with a name of its own.
func invokeRules(operations map[string][]string) []v1.RoleRule {
	used := map[string]bool{}

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
// a name ending in a hash of the action, and a counter when that's taken too. It never returns a used name:
// operation names come from plugins, and a name used twice would make plugin-admin invalid.
func invokeRuleName(action string, used map[string]bool) string {
	readable := strings.Trim(notRuleNameChars.ReplaceAllString(strings.ToLower(strings.TrimPrefix(action, policy.ActionPluginInvokePrefix)), "-"), "-")
	name := "invoke-" + readable
	if readable != "" && len(name) <= 63 && !used[name] {
		return name
	}

	sum := sha256.Sum256([]byte(action))
	hash := hex.EncodeToString(sum[:])[:8]
	for i := 1; ; i++ {
		suffix := "-" + hash
		if i > 1 {
			suffix += fmt.Sprintf("-%d", i)
		}
		// The name is cut to leave room for the suffix, so it never exceeds a rule name's 63 characters
		hashed := strings.TrimRight(name[:min(len(name), 63-len(suffix))], "-") + suffix
		if !used[hashed] {
			return hashed
		}
	}
}

var syncLock sync.Mutex

// Sync writes the shipped objects, with plugin-admin's rules for the operations of the installed plugins.
// It replaces any other object of the same namespace and name (a team's name alone), recreates shipped ones that were
// deleted, and deletes shipped ones that are no longer shipped, e.g. plugin-admin's rule for an operation that's gone,
// or plugin-admin and its binding once no plugin has an operation.
// It never changes a team's members. It's idempotent, and writes everything or nothing.
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

		objects, err := Build(operations)
		if err != nil {
			return err
		}

		var scopeIDs, roleIDs, teamIDs, bindingIDs []string
		for i := range objects.Scopes {
			scope := &objects.Scopes[i]
			scope.UID = objectID("scopes", scope.Namespace, scope.Name)
			scopeIDs = append(scopeIDs, string(scope.UID))
			if err := replaceOthers(txCtx, "scopes", scope.ObjectMeta); err != nil {
				return err
			}
			if err := persisted(db.PersistScope(txCtx, scope, adapter.SourceShipped, nil), &notReady); err != nil {
				return fmt.Errorf("scope %s/%s: %w", scope.Namespace, scope.Name, err)
			}
		}

		for i := range objects.Roles {
			role := &objects.Roles[i]
			role.UID = objectID("roles", role.Namespace, role.Name)
			roleIDs = append(roleIDs, string(role.UID))
			if err := replaceOthers(txCtx, "roles", role.ObjectMeta); err != nil {
				return err
			}
			if err := persisted(db.PersistRole(txCtx, role, adapter.SourceShipped, nil), &notReady); err != nil {
				return fmt.Errorf("role %s/%s: %w", role.Namespace, role.Name, err)
			}
		}

		for i := range objects.Teams {
			team := &objects.Teams[i]
			if err := syncTeam(txCtx, team); err != nil {
				return err
			}
			teamIDs = append(teamIDs, string(team.UID))
		}

		for i := range objects.RoleBindings {
			binding := &objects.RoleBindings[i]
			binding.UID = objectID("role_bindings", binding.Namespace, binding.Name)
			bindingIDs = append(bindingIDs, string(binding.UID))
			if err := replaceOthers(txCtx, "role_bindings", binding.ObjectMeta); err != nil {
				return err
			}
			if err := persisted(db.PersistRoleBinding(txCtx, binding, adapter.SourceShipped, nil), &notReady); err != nil {
				return fmt.Errorf("role binding %s/%s: %w", binding.Namespace, binding.Name, err)
			}
		}

		for _, table := range []struct {
			name string
			ids  []string
		}{{"role_bindings", bindingIDs}, {"roles", roleIDs}, {"scopes", scopeIDs}, {"teams", teamIDs}} {
			if err := deleteUnshipped(txCtx, table.name, table.ids); err != nil {
				return err
			}
		}
		return nil
	})

	errs := append([]error{err}, notReady...)
	if err := dutyRBAC.ReloadPolicy(); err != nil {
		errs = append(errs, fmt.Errorf("failed to reload policy: %w", err))
	}
	return errors.Join(errs...)
}

// syncTeam writes a shipped team, replacing any other team of the same name, and sets its UID.
// Teams have no namespace, so a team's name is unique on its own.
func syncTeam(ctx context.Context, team *v1.Team) error {
	team.UID = objectID("teams", team.Namespace, team.Name)
	err := ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		if err := softDelete(txCtx, "teams", "name = ? AND id != ?", team.Name, team.UID); err != nil {
			return err
		}
		return db.PersistTeam(txCtx, team, adapter.SourceShipped)
	})
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to sync team %s", team.Name)
	}
	return nil
}

// AddToAdmins makes the person a direct member of the shipped admins team, creating the team if it doesn't exist yet.
// Mission Control calls it for admin@local on every start, after RBAC is initialised, so the first admin is never
// locked out.
// The membership's source is SourceShipped, so a team mapper, which only removes the memberships it added, leaves it.
func AddToAdmins(ctx context.Context, personID uuid.UUID) error {
	objects, err := Load()
	if err != nil {
		return err
	}

	team, ok := lo.Find(objects.Teams, func(t v1.Team) bool { return t.Name == TeamAdmins })
	if !ok {
		return fmt.Errorf("shipped.yaml has no %s team", TeamAdmins)
	}

	syncLock.Lock()
	defer syncLock.Unlock()
	if err := syncTeam(ctx, &team); err != nil {
		return err
	}

	if err := ctx.DB().Exec(`INSERT INTO team_members (team_id, person_id, source) VALUES (?, ?, ?)
		ON CONFLICT (team_id, person_id) DO NOTHING`, team.UID, personID, adapter.SourceShipped).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to add %s to team %s", personID, TeamAdmins)
	}

	// Memberships reach the policy through notifications on team_members, which aren't handled yet during startup
	if dutyRBAC.Enforcer() != nil {
		if err := dutyRBAC.AddRoleForUser(personID.String(), string(team.UID)); err != nil {
			return ctx.Oops().Wrapf(err, "failed to add %s to team %s", personID, TeamAdmins)
		}
	}
	return nil
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

// installedOperations returns the operations plugin-admin gets rules for: those of the plugins that have registered,
// and, for installed plugins that haven't registered yet, e.g. right after a restart, the operations plugin-admin
// already has rules for. So a restart doesn't take plugin operations away until each plugin is back.
// A plugin is installed when it's stored or in the registry; plugins proxied by agents are only in the registry.
// A plugin has registered once its manifest arrived, even one without operations.
func installedOperations(ctx context.Context) (map[string][]string, error) {
	operations := plugin.DefaultRegistry.Operations()

	pending := map[string]bool{}
	registered := map[string]bool{}
	for _, entry := range plugin.DefaultRegistry.List() {
		if entry.Name == "" {
			continue
		}
		pending[entry.Name] = true
		if entry.Manifest != nil {
			registered[entry.Name] = true
		}
	}
	rows, err := db.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		pending[row.Name] = true
	}
	for name := range registered {
		delete(pending, name)
	}
	if len(pending) == 0 {
		return operations, nil
	}

	var stored []models.Role
	if err := ctx.DB().Where("namespace = ? AND name = ? AND source = ? AND deleted_at IS NULL",
		adapter.ShippedNamespace, RolePluginAdmin, adapter.SourceShipped).Find(&stored).Error; err != nil {
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

// Start syncs whenever the installed plugins or their operations change, until the context is done.
// Changes that arrive during a sync are synced once it's done.
func Start(ctx context.Context) {
	changed := make(chan struct{}, 1)
	plugin.DefaultRegistry.OnPluginsChanged(func() {
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
					ctx.Logger.Errorf("failed to sync shipped roles after plugins changed: %v", err)
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
