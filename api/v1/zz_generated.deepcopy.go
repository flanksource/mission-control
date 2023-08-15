//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	"github.com/flanksource/duty/types"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AWSConnection) DeepCopyInto(out *AWSConnection) {
	*out = *in
	in.AccessKey.DeepCopyInto(&out.AccessKey)
	in.SecretKey.DeepCopyInto(&out.SecretKey)
	in.SessionToken.DeepCopyInto(&out.SessionToken)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AWSConnection.
func (in *AWSConnection) DeepCopy() *AWSConnection {
	if in == nil {
		return nil
	}
	out := new(AWSConnection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AzureConnection) DeepCopyInto(out *AzureConnection) {
	*out = *in
	if in.ClientID != nil {
		in, out := &in.ClientID, &out.ClientID
		*out = new(types.EnvVar)
		(*in).DeepCopyInto(*out)
	}
	if in.ClientSecret != nil {
		in, out := &in.ClientSecret, &out.ClientSecret
		*out = new(types.EnvVar)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AzureConnection.
func (in *AzureConnection) DeepCopy() *AzureConnection {
	if in == nil {
		return nil
	}
	out := new(AzureConnection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Connection) DeepCopyInto(out *Connection) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Connection.
func (in *Connection) DeepCopy() *Connection {
	if in == nil {
		return nil
	}
	out := new(Connection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Connection) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConnectionList) DeepCopyInto(out *ConnectionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Connection, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConnectionList.
func (in *ConnectionList) DeepCopy() *ConnectionList {
	if in == nil {
		return nil
	}
	out := new(ConnectionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ConnectionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConnectionSpec) DeepCopyInto(out *ConnectionSpec) {
	*out = *in
	in.URL.DeepCopyInto(&out.URL)
	in.Port.DeepCopyInto(&out.Port)
	in.Username.DeepCopyInto(&out.Username)
	in.Password.DeepCopyInto(&out.Password)
	in.Certificate.DeepCopyInto(&out.Certificate)
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(types.JSONStringMap, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConnectionSpec.
func (in *ConnectionSpec) DeepCopy() *ConnectionSpec {
	if in == nil {
		return nil
	}
	out := new(ConnectionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConnectionStatus) DeepCopyInto(out *ConnectionStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConnectionStatus.
func (in *ConnectionStatus) DeepCopy() *ConnectionStatus {
	if in == nil {
		return nil
	}
	out := new(ConnectionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Description) DeepCopyInto(out *Description) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(Labels, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Description.
func (in *Description) DeepCopy() *Description {
	if in == nil {
		return nil
	}
	out := new(Description)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ExecAction) DeepCopyInto(out *ExecAction) {
	*out = *in
	in.Description.DeepCopyInto(&out.Description)
	out.Templatable = in.Templatable
	in.Connections.DeepCopyInto(&out.Connections)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExecAction.
func (in *ExecAction) DeepCopy() *ExecAction {
	if in == nil {
		return nil
	}
	out := new(ExecAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ExecConnections) DeepCopyInto(out *ExecConnections) {
	*out = *in
	if in.AWS != nil {
		in, out := &in.AWS, &out.AWS
		*out = new(AWSConnection)
		(*in).DeepCopyInto(*out)
	}
	if in.GCP != nil {
		in, out := &in.GCP, &out.GCP
		*out = new(GCPConnection)
		(*in).DeepCopyInto(*out)
	}
	if in.Azure != nil {
		in, out := &in.Azure, &out.Azure
		*out = new(AzureConnection)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExecConnections.
func (in *ExecConnections) DeepCopy() *ExecConnections {
	if in == nil {
		return nil
	}
	out := new(ExecConnections)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GCPConnection) DeepCopyInto(out *GCPConnection) {
	*out = *in
	if in.Credentials != nil {
		in, out := &in.Credentials, &out.Credentials
		*out = new(types.EnvVar)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GCPConnection.
func (in *GCPConnection) DeepCopy() *GCPConnection {
	if in == nil {
		return nil
	}
	out := new(GCPConnection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentRule) DeepCopyInto(out *IncidentRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentRule.
func (in *IncidentRule) DeepCopy() *IncidentRule {
	if in == nil {
		return nil
	}
	out := new(IncidentRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IncidentRule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentRuleList) DeepCopyInto(out *IncidentRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]IncidentRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentRuleList.
func (in *IncidentRuleList) DeepCopy() *IncidentRuleList {
	if in == nil {
		return nil
	}
	out := new(IncidentRuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IncidentRuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentRuleStatus) DeepCopyInto(out *IncidentRuleStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentRuleStatus.
func (in *IncidentRuleStatus) DeepCopy() *IncidentRuleStatus {
	if in == nil {
		return nil
	}
	out := new(IncidentRuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in Labels) DeepCopyInto(out *Labels) {
	{
		in := &in
		*out = make(Labels, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Labels.
func (in Labels) DeepCopy() Labels {
	if in == nil {
		return nil
	}
	out := new(Labels)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Permission) DeepCopyInto(out *Permission) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Permission.
func (in *Permission) DeepCopy() *Permission {
	if in == nil {
		return nil
	}
	out := new(Permission)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Playbook) DeepCopyInto(out *Playbook) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Playbook.
func (in *Playbook) DeepCopy() *Playbook {
	if in == nil {
		return nil
	}
	out := new(Playbook)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Playbook) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookAction) DeepCopyInto(out *PlaybookAction) {
	*out = *in
	in.Exec.DeepCopyInto(&out.Exec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookAction.
func (in *PlaybookAction) DeepCopy() *PlaybookAction {
	if in == nil {
		return nil
	}
	out := new(PlaybookAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookParameter) DeepCopyInto(out *PlaybookParameter) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookParameter.
func (in *PlaybookParameter) DeepCopy() *PlaybookParameter {
	if in == nil {
		return nil
	}
	out := new(PlaybookParameter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookResourceFilter) DeepCopyInto(out *PlaybookResourceFilter) {
	*out = *in
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookResourceFilter.
func (in *PlaybookResourceFilter) DeepCopy() *PlaybookResourceFilter {
	if in == nil {
		return nil
	}
	out := new(PlaybookResourceFilter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookSpec) DeepCopyInto(out *PlaybookSpec) {
	*out = *in
	if in.Permissions != nil {
		in, out := &in.Permissions, &out.Permissions
		*out = make([]Permission, len(*in))
		copy(*out, *in)
	}
	if in.Configs != nil {
		in, out := &in.Configs, &out.Configs
		*out = make([]PlaybookResourceFilter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Components != nil {
		in, out := &in.Components, &out.Components
		*out = make([]PlaybookResourceFilter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make([]PlaybookParameter, len(*in))
		copy(*out, *in)
	}
	if in.Actions != nil {
		in, out := &in.Actions, &out.Actions
		*out = make([]PlaybookAction, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookSpec.
func (in *PlaybookSpec) DeepCopy() *PlaybookSpec {
	if in == nil {
		return nil
	}
	out := new(PlaybookSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookStatus) DeepCopyInto(out *PlaybookStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookStatus.
func (in *PlaybookStatus) DeepCopy() *PlaybookStatus {
	if in == nil {
		return nil
	}
	out := new(PlaybookStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Templatable) DeepCopyInto(out *Templatable) {
	*out = *in
	out.Test = in.Test
	out.Display = in.Display
	out.Transform = in.Transform
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Templatable.
func (in *Templatable) DeepCopy() *Templatable {
	if in == nil {
		return nil
	}
	out := new(Templatable)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Template) DeepCopyInto(out *Template) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Template.
func (in *Template) DeepCopy() *Template {
	if in == nil {
		return nil
	}
	out := new(Template)
	in.DeepCopyInto(out)
	return out
}
