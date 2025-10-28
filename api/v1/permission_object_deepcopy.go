package v1

import (
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
)

// DeepCopyInto is a manual deepcopy function for PermissionObject, copying the receiver, writing into out. in must be non-nil.
func (in *PermissionObject) DeepCopyInto(out *PermissionObject) {
	*out = *in

	// Manually copy Selectors fields since it's from an external package
	if in.Playbooks != nil {
		in, out := &in.Playbooks, &out.Playbooks
		*out = make([]types.ResourceSelector, len(*in))
		copy(*out, *in)
	}
	if in.Connections != nil {
		in, out := &in.Connections, &out.Connections
		*out = make([]types.ResourceSelector, len(*in))
		copy(*out, *in)
	}
	if in.Configs != nil {
		in, out := &in.Configs, &out.Configs
		*out = make([]types.ResourceSelector, len(*in))
		copy(*out, *in)
	}
	if in.Components != nil {
		in, out := &in.Components, &out.Components
		*out = make([]types.ResourceSelector, len(*in))
		copy(*out, *in)
	}
	if in.Views != nil {
		in, out := &in.Views, &out.Views
		*out = make([]dutyRBAC.ViewRef, len(*in))
		copy(*out, *in)
	}

	// Copy Scopes
	if in.Scopes != nil {
		in, out := &in.Scopes, &out.Scopes
		*out = make([]dutyRBAC.NamespacedNameIDSelector, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is a manual deepcopy function for PermissionObject, creating a new PermissionObject.
func (in *PermissionObject) DeepCopy() *PermissionObject {
	if in == nil {
		return nil
	}
	out := new(PermissionObject)
	in.DeepCopyInto(out)
	return out
}
