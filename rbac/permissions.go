package rbac

import (
	"github.com/flanksource/duty/rbac/policy"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
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

	AllPermissions = lo.Uniq(append(AllPermissions, adapter.ResourcelessPermissions()...))
}
