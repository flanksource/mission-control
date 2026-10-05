package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	pkgPolicy "github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	gocache "github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type PermissionAdapter struct {
	*gormadapter.Adapter // gorm adapter for `casbin_rules` table

	ctx   context.Context
	cache *gocache.Cache
}

var _ persist.BatchAdapter = &PermissionAdapter{}

const defaultScopeCacheTTL = 1 * time.Minute

func NewPermissionAdapter(ctx context.Context, main *gormadapter.Adapter) persist.Adapter {
	ttl := ctx.Properties().Duration("scope.cache.ttl", defaultScopeCacheTTL)
	return &PermissionAdapter{
		ctx:     ctx,
		Adapter: main,
		cache:   gocache.New(ttl, ttl*2),
	}
}

func (a *PermissionAdapter) LoadPolicy(model model.Model) error {
	// The cache only spares lookups of the same scope within a load, so scope changes apply on the next load
	a.cache.Flush()

	if err := a.loadBasePolicy(model); err != nil {
		return err
	}

	// Generate ABAC companion rules for policies loaded from casbin_rules
	// (which includes default policies from policies.yaml).
	// Without this, default RBAC rules like "viewer, views, read" only work
	// for plain RBAC checks (string objects) but fail for HasPermission()
	// ABAC checks where r.obj is *ABACAttribute.
	if err := generateABACCompanions(model); err != nil {
		return err
	}

	var permissions []models.Permission
	if err := a.ctx.DB().Where("deleted_at IS NULL AND error IS NULL").Find(&permissions).Error; err != nil {
		return fmt.Errorf("failed to load permissions: %w", err)
	}

	for _, permission := range permissions {
		// Expand scope references in object_selector before converting to Casbin rules
		expandedPerms, err := ExpandPermissionScopes(a.ctx, a.cache, permission)
		if err != nil {
			var validationErr *scopeExpansionValidationError
			if errors.As(err, &validationErr) {
				// Persist validation error to database
				if updateErr := a.ctx.DB().Model(&permission).Update("error", err.Error()).Error; updateErr != nil {
					return fmt.Errorf("failed to update permission error: %w", updateErr)
				}

				continue // Skip this permission
			}

			return err
		}

		if len(expandedPerms) == 0 {
			policies := PermissionToCasbinRule(permission)
			for _, policy := range policies {
				if err := persist.LoadPolicyArray(policy, model); err != nil {
					return err
				}
			}
		} else {
			// A permission that targets a scope will generate multiple expanded permissions
			// If the targeted scope as N scopes, then this will generate N permissions
			// everything about the permission remains the same apart from the object_selector

			for _, expandedPerm := range expandedPerms {
				marshalled, err := json.Marshal(expandedPerm)
				if err != nil {
					return err
				}

				newPerm := permission
				newPerm.ObjectSelector = marshalled
				policies := PermissionToCasbinRule(newPerm)
				for _, policy := range policies {
					if err := persist.LoadPolicyArray(policy, model); err != nil {
						return err
					}
				}
			}
		}
	}

	var permissionGroups []models.PermissionGroup
	if err := a.ctx.DB().Where("deleted_at IS NULL").Find(&permissionGroups).Error; err != nil {
		return fmt.Errorf("failed to load permissions: %w", err)
	}

	for _, pg := range permissionGroups {
		policies, err := a.permissionGroupToCasbinRule(pg)
		if err != nil {
			return err
		}

		for _, policy := range policies {
			if err := persist.LoadPolicyArray(policy, model); err != nil {
				return err
			}
		}
	}

	return a.loadRoleBindings(model)
}

// loadRoleBindings validates every Scope, Role and RoleBinding, records why each invalid one isn't in effect,
// and loads the policies of the valid bindings. An invalid object grants nothing, deny rules included.
// Each Scope's membership is rebuilt when it changed, e.g. because an agent it names was re-registered,
// and cleared when it's invalid.
func (a *PermissionAdapter) loadRoleBindings(m model.Model) error {
	var scopes []models.Scope
	if err := a.ctx.DB().Where("deleted_at IS NULL").Find(&scopes).Error; err != nil {
		return fmt.Errorf("failed to load scopes: %w", err)
	}

	for _, scope := range scopes {
		err := SyncScopeMembership(a.ctx, scope)
		if err != nil && !IsValidationError(err) {
			// Nothing reads membership yet, so failing to store it mustn't change what the Scope grants
			a.ctx.Logger.Errorf("failed to sync the membership of scope %s/%s: %v", scope.Namespace, scope.Name, err)
			err = ValidateStoredScope(a.ctx, scope)
		}
		if err := recordValidity(a.ctx, scope.TableName(), scope.ID, scope.Namespace, scope.Name, scope.Source, scope.Error, scope.ErrorReason, err); err != nil {
			return err
		}
	}

	var roles []models.Role
	if err := a.ctx.DB().Where("deleted_at IS NULL").Find(&roles).Error; err != nil {
		return fmt.Errorf("failed to load roles: %w", err)
	}

	for _, role := range roles {
		_, err := ValidateRole(a.ctx, a.cache, role)
		if err := recordValidity(a.ctx, role.TableName(), role.ID, role.Namespace, role.Name, role.Source, role.Error, role.ErrorReason, err); err != nil {
			return err
		}
	}
	rolesByName := lo.KeyBy(roles, func(r models.Role) string { return r.Namespace + "/" + r.Name })

	var bindings []models.RoleBinding
	if err := a.ctx.DB().Where("deleted_at IS NULL").Find(&bindings).Error; err != nil {
		return fmt.Errorf("failed to load role bindings: %w", err)
	}

	reports := map[uuid.UUID]unappliedReport{}
	for _, binding := range bindings {
		var role *models.Role
		if r, ok := rolesByName[binding.Namespace+"/"+binding.Role]; ok {
			role = &r
		}

		policies, unapplied, err := a.roleBindingToCasbinRules(binding, role)
		if err := recordValidity(a.ctx, binding.TableName(), binding.ID, binding.Namespace, binding.Name, binding.Source, binding.Error, binding.ErrorReason, err); err != nil {
			return err
		}

		report := unappliedReport{binding: binding, report: strings.Join(lo.Map(unapplied, func(u UnappliedRule, _ int) string { return u.String() }), "; ")}
		if role != nil {
			report.roleSource = role.Source
		}
		reports[binding.ID] = report

		for _, policy := range policies {
			if err := persist.LoadPolicyArray(policy, m); err != nil {
				return err
			}
		}
	}

	recordUnappliedReports(reports)
	return nil
}

// unappliedReport is what a binding reports about the allow rules that don't apply through it.
type unappliedReport struct {
	binding    models.RoleBinding
	roleSource string
	report     string
}

// unappliedReports are the reports of every binding as of the last policy load, by binding id.
var unappliedReports = struct {
	sync.Mutex
	byBinding map[uuid.UUID]unappliedReport
}{byBinding: map[uuid.UUID]unappliedReport{}}

// recordUnappliedReports notifies ValidityChanged of every binding whose report changed since the last load,
// and of its role, whose status lists the bindings reporting one of its rules. A Role change can change what its
// bindings report without changing whether they're valid.
func recordUnappliedReports(reports map[uuid.UUID]unappliedReport) {
	unappliedReports.Lock()
	previous := unappliedReports.byBinding
	unappliedReports.byBinding = reports
	unappliedReports.Unlock()

	if ValidityChanged == nil {
		return
	}

	notifyRole := func(r unappliedReport) {
		if r.roleSource != "" {
			ValidityChanged("roles", r.binding.Namespace, r.binding.Role, r.roleSource)
		}
	}

	for id, current := range reports {
		if old := previous[id]; old.report != current.report {
			ValidityChanged("role_bindings", current.binding.Namespace, current.binding.Name, current.binding.Source)
			notifyRole(current)
		}
	}

	for id, old := range previous {
		if _, ok := reports[id]; !ok && old.report != "" {
			notifyRole(old)
		}
	}
}

// ValidityChanged is called when a Scope, Role or RoleBinding (by table) becomes valid or invalid, or why it's invalid
// changes, e.g. because a Scope it references was deleted. The server sets it to reconcile the object's CRD again,
// so its Ready condition follows.
var ValidityChanged func(table, namespace, name, source string)

// recordValidity stores why a Scope, Role or RoleBinding isn't in effect, or clears it when it's valid.
// It only writes when that changed, since every write reloads the policy. Errors other than validation errors are returned.
func recordValidity(ctx context.Context, table string, id uuid.UUID, namespace, name, source string, currentError, currentReason *string, err error) error {
	if err != nil && !IsValidationError(err) {
		return err
	}

	var message, reason *string
	if err != nil {
		message, reason = lo.ToPtr(err.Error()), lo.ToPtr(InvalidReason(err))
	}

	if lo.FromPtr(message) == lo.FromPtr(currentError) && lo.FromPtr(reason) == lo.FromPtr(currentReason) {
		return nil
	}

	if err := ctx.DB().Table(table).Where("id = ?", id).
		UpdateColumns(map[string]any{"error": message, "error_reason": reason}).Error; err != nil {
		ctx.Errorf("failed to record the validity of %s %s: %v", table, id, err)
	}

	if ValidityChanged != nil {
		ValidityChanged(table, namespace, name, source)
	}

	return nil
}

func (a *PermissionAdapter) SavePolicy(m model.Model) error {
	tx := a.ctx.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}

	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&gormadapter.CasbinRule{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	var lines []gormadapter.CasbinRule
	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			lines = append(lines, casbinRuleFromPolicy(ptype, rule))
		}
	}
	for ptype, ast := range m["g"] {
		for _, rule := range ast.Policy {
			lines = append(lines, casbinRuleFromPolicy(ptype, rule))
		}
	}

	if len(lines) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lines).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

func (a *PermissionAdapter) loadBasePolicy(m model.Model) error {
	var lines []gormadapter.CasbinRule
	if err := a.ctx.DB().Order("id").Find(&lines).Error; err != nil {
		return err
	}

	for _, line := range lines {
		if err := loadCasbinRule(line, m); err != nil {
			return err
		}
	}

	return nil
}

func loadCasbinRule(line gormadapter.CasbinRule, m model.Model) error {
	policy := []string{line.Ptype, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5}
	index := len(policy) - 1
	for index > 0 && policy[index] == "" {
		index--
	}
	policy = policy[:index+1]
	return persist.LoadPolicyArray(policy, m)
}

func casbinRuleFromPolicy(ptype string, rule []string) gormadapter.CasbinRule {
	line := gormadapter.CasbinRule{Ptype: ptype}
	if len(rule) > 0 {
		line.V0 = rule[0]
	}
	if len(rule) > 1 {
		line.V1 = rule[1]
	}
	if len(rule) > 2 {
		line.V2 = rule[2]
	}
	if len(rule) > 3 {
		line.V3 = rule[3]
	}
	if len(rule) > 4 {
		line.V4 = rule[4]
	}
	if len(rule) > 5 {
		line.V5 = rule[5]
	}
	return line
}

func PermissionToCasbinRule(permission models.Permission) [][]string {
	var policies [][]string
	for _, action := range expandActions(strings.Split(permission.Action, ",")) {
		policies = append(policies, createPolicy(permission, action))

		if objectSelector := rbacToABACObjectSelector(permission, action); objectSelector != nil {
			abacPermission := permission
			abacPermission.Object = ""
			abacPermission.ObjectSelector = objectSelector
			policies = append(policies, createPolicy(abacPermission, action))
		}
	}

	return policies
}

// expandActions returns the actions matched by the given patterns: every built-in action a
// pattern matches (e.g. "*" or "playbook:*"), plus any non built-in action named literally.
func expandActions(patterns []string) []string {
	var actions []string
	seen := map[string]struct{}{}

	add := func(action string) {
		if action == "" {
			return
		}
		if _, ok := seen[action]; ok {
			return
		}
		seen[action] = struct{}{}
		actions = append(actions, action)
	}

	for _, action := range pkgPolicy.AllActions {
		if collections.MatchItems(action, patterns...) {
			add(action)
		}
	}

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || collections.MatchItems(pattern, pkgPolicy.AllActions...) {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			continue
		}
		add(pattern)
	}

	return actions
}

// createPolicy generates a Casbin policy rule from a permission.
func createPolicy(permission models.Permission, action string) []string {
	policy := []string{
		"p",
		permission.Principal(),
		permission.GetObject(),
		action,
		permission.Effect(),
		permission.Condition(),
		permission.ID.String(),
	}
	return policy
}

// rbacToABACObjectSelector returns object selectors (v1.PermissionObject) in JSON
// for ABAC policies from a global permission.
func rbacToABACObjectSelector(permission models.Permission, action string) []byte {
	return pkgPolicy.ABACObjectSelector(permission.Object, action)
}

// Helper function for finding namespaced resources by selector
func (a *PermissionAdapter) findNamespacedResources(tableName string, selectors []v1.PermissionGroupSelector) ([]string, error) {
	var clauses []clause.Expression
	for _, selector := range selectors {
		if selector.Empty() {
			continue
		}

		var conditions = []clause.Expression{
			clause.Eq{Column: "deleted_at", Value: nil},
		}

		if !selector.Wildcard() {
			if selector.Namespace != "" {
				conditions = append(conditions, clause.Eq{Column: "namespace", Value: selector.Namespace})
			}
			if selector.Name != "" && selector.Name != "*" {
				conditions = append(conditions, clause.Eq{Column: "name", Value: selector.Name})
			}
		}

		clauses = append(clauses, clause.And(conditions...))
	}

	if len(clauses) == 0 {
		return nil, nil
	}

	var ids []string
	if err := a.ctx.DB().Select("id").Table(tableName).Clauses(clause.Or(clauses...)).Find(&ids).Error; err != nil {
		return nil, err
	}

	return ids, nil
}

func (a *PermissionAdapter) permissionGroupToCasbinRule(permission models.PermissionGroup) ([][]string, error) {
	var subject v1.PermissionGroupSubjects
	if err := json.Unmarshal(permission.Selectors, &subject); err != nil {
		return nil, err
	}

	allSubjects, err := a.resolveSubjects(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve subjects for permission group %s: %w", permission.Name, err)
	}

	var policies [][]string
	for _, subject := range allSubjects {
		policies = append(policies, []string{"g", subject, permission.Name, "", "", ""})
	}

	return policies, nil
}

// roleBindingToCasbinRules compiles a binding into:
//
//   - the rules that apply through it, filed under binding:<ns>/<name> (see compiledRuleToCasbinRules)
//   - an assignment of the binding to each of its static subjects: g, <subject>, binding:<ns>/<name>
//
// Users of external identity providers are assigned to the binding when their token's claims match (see auth/federation.go).
//
// It also returns the allow rules that don't apply through the binding, and a validation error when the binding
// isn't Ready. A binding that's invalid on its own, or whose role is missing or invalid, has no policies.
// One whose constraint applies no allow rule still has the policies of its role's deny rules.
func (a *PermissionAdapter) roleBindingToCasbinRules(binding models.RoleBinding, role *models.Role) ([][]string, []UnappliedRule, error) {
	spec, err := RoleBindingSpec(binding)
	if err != nil {
		return nil, nil, err
	}

	if role == nil {
		return nil, nil, NewInvalid(ReasonRoleNotFound, "role %s/%s not found", binding.Namespace, binding.Role)
	}

	compiled, compileErr := CompileBinding(a.ctx, a.cache, binding, *role)
	if compileErr != nil && !IsValidationError(compileErr) {
		return nil, nil, compileErr
	}

	principal := binding.Principal()
	var policies [][]string
	for _, rule := range compiled.Rules {
		rulePolicies, err := compiledRuleToCasbinRules(principal, rule)
		if err != nil {
			return nil, nil, err
		}
		policies = append(policies, rulePolicies...)
	}

	if len(policies) == 0 {
		return nil, compiled.Unapplied, compileErr
	}

	subjects := spec.Subjects
	allSubjects, err := a.resolveNamespacedSubjects(subjects.PermissionGroupSubjects)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve subjects for role binding %s/%s: %w", binding.Namespace, binding.Name, err)
	}

	people, err := a.resolvePeopleByEmail(subjects.People)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve people for role binding %s/%s: %w", binding.Namespace, binding.Name, err)
	}

	teams, err := a.resolveTeams(subjects.Teams)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve teams for role binding %s/%s: %w", binding.Namespace, binding.Name, err)
	}

	allSubjects = append(allSubjects, people...)
	allSubjects = append(allSubjects, teams...)
	allSubjects = append(allSubjects, subjects.Roles...)

	for _, subject := range lo.Uniq(allSubjects) {
		policies = append(policies, []string{"g", subject, principal, "", "", ""})
	}

	return policies, compiled.Unapplied, compileErr
}

// RoleBindingSpec returns the spec of a stored binding, validated on its own.
func RoleBindingSpec(binding models.RoleBinding) (v1.RoleBindingSpec, error) {
	spec := v1.RoleBindingSpec{Role: binding.Role}
	if len(binding.Subjects) > 0 {
		if err := json.Unmarshal(binding.Subjects, &spec.Subjects); err != nil {
			return spec, NewValidationError("role binding %s/%s: invalid subjects: %v", binding.Namespace, binding.Name, err)
		}
	}

	constraint, err := bindingConstraint(binding)
	if err != nil {
		return spec, err
	}
	spec.Constraint = constraint

	if err := spec.Validate(); err != nil {
		return spec, NewValidationError("role binding %s/%s: %v", binding.Namespace, binding.Name, err)
	}

	return spec, nil
}

// resolvePeopleByEmail returns the ids of the Mission Control users with the given emails.
// Agents and users of external identity providers aren't people.
func (a *PermissionAdapter) resolvePeopleByEmail(emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}

	var ids []string
	err := a.ctx.DB().Select("id").Model(&models.Person{}).
		Where("deleted_at IS NULL").
		Where("(type IS NULL OR type NOT IN ?)", []string{"agent", "federated"}).
		Where("email IN ?", emails).
		Find(&ids).Error
	return ids, err
}

func (a *PermissionAdapter) resolveTeams(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	var ids []string
	err := a.ctx.DB().Select("id").Model(&models.Team{}).
		Where("deleted_at IS NULL").
		Where("name IN ?", names).
		Find(&ids).Error
	return ids, err
}

// resolveNamespacedSubjects resolves the resource subjects: playbooks, notifications, topologies, scrapers and canaries.
func (a *PermissionAdapter) resolveNamespacedSubjects(subject v1.PermissionGroupSubjects) ([]string, error) {
	var allSubjects []string

	namespacedSubjects := map[string][]v1.PermissionGroupSelector{
		"notifications":   subject.Notifications,
		"playbooks":       subject.Playbooks,
		"topologies":      subject.Topologies,
		"config_scrapers": subject.Scrapers,
		"canaries":        subject.Canaries,
	}

	for modelName, selectors := range namespacedSubjects {
		if len(selectors) == 0 {
			continue
		}

		ids, err := a.findNamespacedResources(modelName, selectors)
		if err != nil {
			return nil, fmt.Errorf("failed to find %s subjects: %w", modelName, err)
		}

		allSubjects = append(allSubjects, ids...)
	}

	return allSubjects, nil
}

// resolveSubjects resolves the subjects of a PermissionGroup to casbin subjects (ids or roles).
// People are matched by email or name, and ["*"] selects everyone.
func (a *PermissionAdapter) resolveSubjects(subject v1.PermissionGroupSubjects) ([]string, error) {
	allSubjects, err := a.resolveNamespacedSubjects(subject)
	if err != nil {
		return nil, err
	}

	if len(subject.People) > 0 {
		wildcard := len(subject.People) == 1 && subject.People[0] == "*"
		if wildcard {
			allSubjects = append(allSubjects, pkgPolicy.RoleEveryone)
		} else {
			var personIDs []string
			query := a.ctx.DB().Select("id").Model(&models.Person{}).
				Where("deleted_at IS NULL").
				Where("type IS DISTINCT FROM 'agent'").
				Where("email IS NOT NULL"). // Excludes system user
				Where("email IN ? OR name IN ?", subject.People, subject.People)
			if err := query.Find(&personIDs).Error; err != nil {
				return nil, err
			}

			allSubjects = append(allSubjects, personIDs...)
		}
	}

	if len(subject.Teams) > 0 {
		var teamIDs []string
		if err := a.ctx.DB().Select("id").Model(&models.Team{}).
			Where("deleted_at IS NULL").
			Where("name IN ?", subject.Teams).
			Find(&teamIDs).Error; err != nil {
			return nil, err
		}

		allSubjects = append(allSubjects, teamIDs...)
	}

	return allSubjects, nil
}

// generateABACCompanions scans policies already loaded into the model (from casbin_rules)
// and generates in-memory ABAC companion rules for any that match an ABAC-eligible
// object+action pair. This ensures default policies like "viewer, views, read" also
// work with HasPermission() where r.obj is *ABACAttribute, not a string.
func generateABACCompanions(m model.Model) error {
	pModel, ok := m["p"]
	if !ok {
		return nil
	}

	assertion, ok := pModel["p"]
	if !ok {
		return nil
	}

	// Collect first to avoid modifying the slice during iteration
	var companions [][]string
	for _, pol := range assertion.Policy {
		if len(pol) < 6 {
			continue
		}

		obj, act := pol[1], pol[2]
		if selector := pkgPolicy.ABACObjectSelector(obj, act); selector != nil {
			companions = append(companions, []string{"p", pol[0], "*", act, pol[3], selectorCondition(selector), pol[5]})
		}
	}

	for _, companion := range companions {
		if err := persist.LoadPolicyArray(companion, m); err != nil {
			return err
		}
	}

	return nil
}

func selectorCondition(selector []byte) string {
	return fmt.Sprintf(`matchResourceSelector(r.obj, %q)`, string(selector))
}
