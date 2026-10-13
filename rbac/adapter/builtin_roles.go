package adapter

import (
	"slices"

	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
)

// BuiltInRoles are the roles of earlier versions, granted by duty's policies.yaml. They grant nothing
// (specs/authorization/overview.md, "No built-in roles"): no rule is filed under one, and no subject is assigned to one.
var BuiltInRoles = []string{
	policy.RoleAdmin, policy.RoleEveryone, policy.RoleGuest, policy.RoleViewer,
	policy.RoleEditor, policy.RoleCommander, policy.RoleResponder, policy.RoleAgent,
}

// namesBuiltInRole reports whether a casbin rule grants a built-in role, or assigns a subject to or through one.
func namesBuiltInRole(ptype string, rule []string) bool {
	if len(rule) == 0 {
		return false
	}
	if slices.Contains(BuiltInRoles, rule[0]) {
		return true
	}
	return ptype == "g" && len(rule) > 1 && slices.Contains(BuiltInRoles, rule[1])
}

// DeleteBuiltInRoleRules deletes the stored rules of the built-in roles and every assignment to or through one,
// e.g. a person's admin role. They're never converted into RoleBindings (specs/authorization/design/bootstrap.md).
func DeleteBuiltInRoleRules(ctx context.Context) error {
	result := ctx.DB().
		Where("(ptype = 'p' AND v0 IN ?) OR (ptype = 'g' AND (v0 IN ? OR v1 IN ?))", BuiltInRoles, BuiltInRoles, BuiltInRoles).
		Delete(&gormadapter.CasbinRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		ctx.Infof("deleted %d rules and assignments of built-in roles", result.RowsAffected)
	}
	return nil
}

// dropBuiltInRoleRules removes every rule naming a built-in role from the model.
func dropBuiltInRoleRules(m model.Model) error {
	for _, sec := range []string{"p", "g"} {
		for ptype, ast := range m[sec] {
			var dropped [][]string
			for _, rule := range ast.Policy {
				if namesBuiltInRole(sec, rule) {
					dropped = append(dropped, rule)
				}
			}
			for _, rule := range dropped {
				if _, err := m.RemovePolicy(sec, ptype, rule); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// AddPolicy stores the rule, unless it names a built-in role.
func (a *PermissionAdapter) AddPolicy(sec, ptype string, rule []string) error {
	if namesBuiltInRole(sec, rule) {
		return nil
	}
	return a.Adapter.AddPolicy(sec, ptype, rule)
}

// AddPolicies stores the rules, leaving out those that name a built-in role.
func (a *PermissionAdapter) AddPolicies(sec, ptype string, rules [][]string) error {
	rules = slices.DeleteFunc(slices.Clone(rules), func(rule []string) bool { return namesBuiltInRole(sec, rule) })
	if len(rules) == 0 {
		return nil
	}
	return a.Adapter.AddPolicies(sec, ptype, rules)
}
