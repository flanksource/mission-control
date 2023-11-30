//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	"encoding/json"
	"github.com/flanksource/duty/types"
	runtime "k8s.io/apimachinery/pkg/runtime"
	timex "time"
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
func (in *Authentication) DeepCopyInto(out *Authentication) {
	*out = *in
	in.Username.DeepCopyInto(&out.Username)
	in.Password.DeepCopyInto(&out.Password)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Authentication.
func (in *Authentication) DeepCopy() *Authentication {
	if in == nil {
		return nil
	}
	out := new(Authentication)
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
func (in *ExecAction) DeepCopyInto(out *ExecAction) {
	*out = *in
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
func (in *GitOpsAction) DeepCopyInto(out *GitOpsAction) {
	*out = *in
	out.Repo = in.Repo
	out.Commit = in.Commit
	if in.PullRequest != nil {
		in, out := &in.PullRequest, &out.PullRequest
		*out = new(GitOpsActionPR)
		(*in).DeepCopyInto(*out)
	}
	if in.Patches != nil {
		in, out := &in.Patches, &out.Patches
		*out = make([]GitOpsActionPatch, len(*in))
		copy(*out, *in)
	}
	if in.Files != nil {
		in, out := &in.Files, &out.Files
		*out = make([]GitOpsActionFile, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsAction.
func (in *GitOpsAction) DeepCopy() *GitOpsAction {
	if in == nil {
		return nil
	}
	out := new(GitOpsAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitOpsActionCommit) DeepCopyInto(out *GitOpsActionCommit) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsActionCommit.
func (in *GitOpsActionCommit) DeepCopy() *GitOpsActionCommit {
	if in == nil {
		return nil
	}
	out := new(GitOpsActionCommit)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitOpsActionFile) DeepCopyInto(out *GitOpsActionFile) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsActionFile.
func (in *GitOpsActionFile) DeepCopy() *GitOpsActionFile {
	if in == nil {
		return nil
	}
	out := new(GitOpsActionFile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitOpsActionPR) DeepCopyInto(out *GitOpsActionPR) {
	*out = *in
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsActionPR.
func (in *GitOpsActionPR) DeepCopy() *GitOpsActionPR {
	if in == nil {
		return nil
	}
	out := new(GitOpsActionPR)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitOpsActionPatch) DeepCopyInto(out *GitOpsActionPatch) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsActionPatch.
func (in *GitOpsActionPatch) DeepCopy() *GitOpsActionPatch {
	if in == nil {
		return nil
	}
	out := new(GitOpsActionPatch)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitOpsActionRepo) DeepCopyInto(out *GitOpsActionRepo) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitOpsActionRepo.
func (in *GitOpsActionRepo) DeepCopy() *GitOpsActionRepo {
	if in == nil {
		return nil
	}
	out := new(GitOpsActionRepo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPAction) DeepCopyInto(out *HTTPAction) {
	*out = *in
	in.HTTPConnection.DeepCopyInto(&out.HTTPConnection)
	if in.Headers != nil {
		in, out := &in.Headers, &out.Headers
		*out = make([]types.EnvVar, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPAction.
func (in *HTTPAction) DeepCopy() *HTTPAction {
	if in == nil {
		return nil
	}
	out := new(HTTPAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HTTPConnection) DeepCopyInto(out *HTTPConnection) {
	*out = *in
	in.Authentication.DeepCopyInto(&out.Authentication)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HTTPConnection.
func (in *HTTPConnection) DeepCopy() *HTTPConnection {
	if in == nil {
		return nil
	}
	out := new(HTTPConnection)
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
func (in *Notification) DeepCopyInto(out *Notification) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Notification.
func (in *Notification) DeepCopy() *Notification {
	if in == nil {
		return nil
	}
	out := new(Notification)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Notification) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationAction) DeepCopyInto(out *NotificationAction) {
	*out = *in
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationAction.
func (in *NotificationAction) DeepCopy() *NotificationAction {
	if in == nil {
		return nil
	}
	out := new(NotificationAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationList) DeepCopyInto(out *NotificationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Notification, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationList.
func (in *NotificationList) DeepCopy() *NotificationList {
	if in == nil {
		return nil
	}
	out := new(NotificationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NotificationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationRecipientSpec) DeepCopyInto(out *NotificationRecipientSpec) {
	*out = *in
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationRecipientSpec.
func (in *NotificationRecipientSpec) DeepCopy() *NotificationRecipientSpec {
	if in == nil {
		return nil
	}
	out := new(NotificationRecipientSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationSpec) DeepCopyInto(out *NotificationSpec) {
	*out = *in
	if in.Events != nil {
		in, out := &in.Events, &out.Events
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.To.DeepCopyInto(&out.To)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationSpec.
func (in *NotificationSpec) DeepCopy() *NotificationSpec {
	if in == nil {
		return nil
	}
	out := new(NotificationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationStatus) DeepCopyInto(out *NotificationStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationStatus.
func (in *NotificationStatus) DeepCopy() *NotificationStatus {
	if in == nil {
		return nil
	}
	out := new(NotificationStatus)
	in.DeepCopyInto(out)
	return out
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
	if in.delay != nil {
		in, out := &in.delay, &out.delay
		*out = new(timex.Duration)
		**out = **in
	}
	if in.timeout != nil {
		in, out := &in.timeout, &out.timeout
		*out = new(timex.Duration)
		**out = **in
	}
	if in.Exec != nil {
		in, out := &in.Exec, &out.Exec
		*out = new(ExecAction)
		(*in).DeepCopyInto(*out)
	}
	if in.GitOps != nil {
		in, out := &in.GitOps, &out.GitOps
		*out = new(GitOpsAction)
		(*in).DeepCopyInto(*out)
	}
	if in.HTTP != nil {
		in, out := &in.HTTP, &out.HTTP
		*out = new(HTTPAction)
		(*in).DeepCopyInto(*out)
	}
	if in.SQL != nil {
		in, out := &in.SQL, &out.SQL
		*out = new(SQLAction)
		**out = **in
	}
	if in.Pod != nil {
		in, out := &in.Pod, &out.Pod
		*out = new(PodAction)
		(*in).DeepCopyInto(*out)
	}
	if in.Notification != nil {
		in, out := &in.Notification, &out.Notification
		*out = new(NotificationAction)
		(*in).DeepCopyInto(*out)
	}
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
func (in *PlaybookApproval) DeepCopyInto(out *PlaybookApproval) {
	*out = *in
	in.Approvers.DeepCopyInto(&out.Approvers)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookApproval.
func (in *PlaybookApproval) DeepCopy() *PlaybookApproval {
	if in == nil {
		return nil
	}
	out := new(PlaybookApproval)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookApprovers) DeepCopyInto(out *PlaybookApprovers) {
	*out = *in
	if in.People != nil {
		in, out := &in.People, &out.People
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Teams != nil {
		in, out := &in.Teams, &out.Teams
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookApprovers.
func (in *PlaybookApprovers) DeepCopy() *PlaybookApprovers {
	if in == nil {
		return nil
	}
	out := new(PlaybookApprovers)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookEvent) DeepCopyInto(out *PlaybookEvent) {
	*out = *in
	if in.Canary != nil {
		in, out := &in.Canary, &out.Canary
		*out = make([]PlaybookEventDetail, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Component != nil {
		in, out := &in.Component, &out.Component
		*out = make([]PlaybookEventDetail, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookEvent.
func (in *PlaybookEvent) DeepCopy() *PlaybookEvent {
	if in == nil {
		return nil
	}
	out := new(PlaybookEvent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookEventDetail) DeepCopyInto(out *PlaybookEventDetail) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookEventDetail.
func (in *PlaybookEventDetail) DeepCopy() *PlaybookEventDetail {
	if in == nil {
		return nil
	}
	out := new(PlaybookEventDetail)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookList) DeepCopyInto(out *PlaybookList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Playbook, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlaybookList.
func (in *PlaybookList) DeepCopy() *PlaybookList {
	if in == nil {
		return nil
	}
	out := new(PlaybookList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PlaybookList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlaybookParameter) DeepCopyInto(out *PlaybookParameter) {
	*out = *in
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
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
	in.On.DeepCopyInto(&out.On)
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
	if in.Checks != nil {
		in, out := &in.Checks, &out.Checks
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
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Actions != nil {
		in, out := &in.Actions, &out.Actions
		*out = make([]PlaybookAction, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Approval != nil {
		in, out := &in.Approval, &out.Approval
		*out = new(PlaybookApproval)
		(*in).DeepCopyInto(*out)
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
func (in *PodAction) DeepCopyInto(out *PodAction) {
	*out = *in
	if in.Spec != nil {
		in, out := &in.Spec, &out.Spec
		*out = make(json.RawMessage, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodAction.
func (in *PodAction) DeepCopy() *PodAction {
	if in == nil {
		return nil
	}
	out := new(PodAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SQLAction) DeepCopyInto(out *SQLAction) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SQLAction.
func (in *SQLAction) DeepCopy() *SQLAction {
	if in == nil {
		return nil
	}
	out := new(SQLAction)
	in.DeepCopyInto(out)
	return out
}
