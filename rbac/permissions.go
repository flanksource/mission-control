package rbac

import (
	"github.com/flanksource/duty/rbac/policy"
)

var (
	AllPermissions []policy.Permission
)

func init() {
	for _, obj := range policy.AllObjects {
		for _, act := range policy.AllActions {
			AllPermissions = append(AllPermissions, policy.NewPermission([]string{"", obj, act}))
		}
	}
}
