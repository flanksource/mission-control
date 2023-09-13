//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package api

import (
	timex "time"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AutoClose) DeepCopyInto(out *AutoClose) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AutoClose.
func (in *AutoClose) DeepCopy() *AutoClose {
	if in == nil {
		return nil
	}
	out := new(AutoClose)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ComponentSelector) DeepCopyInto(out *ComponentSelector) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Types != nil {
		in, out := &in.Types, &out.Types
		*out = make(Items, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ComponentSelector.
func (in *ComponentSelector) DeepCopy() *ComponentSelector {
	if in == nil {
		return nil
	}
	out := new(ComponentSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Filter) DeepCopyInto(out *Filter) {
	*out = *in
	if in.Status != nil {
		in, out := &in.Status, &out.Status
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Severity != nil {
		in, out := &in.Severity, &out.Severity
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Category != nil {
		in, out := &in.Category, &out.Category
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Age != nil {
		in, out := &in.Age, &out.Age
		*out = new(timex.Duration)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Filter.
func (in *Filter) DeepCopy() *Filter {
	if in == nil {
		return nil
	}
	out := new(Filter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubIssue) DeepCopyInto(out *GithubIssue) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubIssue.
func (in *GithubIssue) DeepCopy() *GithubIssue {
	if in == nil {
		return nil
	}
	out := new(GithubIssue)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HoursOfOperation) DeepCopyInto(out *HoursOfOperation) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HoursOfOperation.
func (in *HoursOfOperation) DeepCopy() *HoursOfOperation {
	if in == nil {
		return nil
	}
	out := new(HoursOfOperation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentResponders) DeepCopyInto(out *IncidentResponders) {
	*out = *in
	if in.Email != nil {
		in, out := &in.Email, &out.Email
		*out = make([]Email, len(*in))
		copy(*out, *in)
	}
	if in.Jira != nil {
		in, out := &in.Jira, &out.Jira
		*out = make([]Jira, len(*in))
		copy(*out, *in)
	}
	if in.AWS != nil {
		in, out := &in.AWS, &out.AWS
		*out = make([]CloudProvider, len(*in))
		copy(*out, *in)
	}
	if in.AMS != nil {
		in, out := &in.AMS, &out.AMS
		*out = make([]CloudProvider, len(*in))
		copy(*out, *in)
	}
	if in.GCP != nil {
		in, out := &in.GCP, &out.GCP
		*out = make([]CloudProvider, len(*in))
		copy(*out, *in)
	}
	if in.ServiceNow != nil {
		in, out := &in.ServiceNow, &out.ServiceNow
		*out = make([]ServiceNow, len(*in))
		copy(*out, *in)
	}
	if in.Slack != nil {
		in, out := &in.Slack, &out.Slack
		*out = make([]Slack, len(*in))
		copy(*out, *in)
	}
	if in.Teams != nil {
		in, out := &in.Teams, &out.Teams
		*out = make([]TeamsChannel, len(*in))
		copy(*out, *in)
	}
	if in.TeamsUser != nil {
		in, out := &in.TeamsUser, &out.TeamsUser
		*out = make([]TeamsUser, len(*in))
		copy(*out, *in)
	}
	if in.GithubIssue != nil {
		in, out := &in.GithubIssue, &out.GithubIssue
		*out = make([]GithubIssue, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentResponders.
func (in *IncidentResponders) DeepCopy() *IncidentResponders {
	if in == nil {
		return nil
	}
	out := new(IncidentResponders)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentRuleSpec) DeepCopyInto(out *IncidentRuleSpec) {
	*out = *in
	if in.Components != nil {
		in, out := &in.Components, &out.Components
		*out = make([]ComponentSelector, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	out.Template = in.Template
	in.Filter.DeepCopyInto(&out.Filter)
	if in.HoursOfOperation != nil {
		in, out := &in.HoursOfOperation, &out.HoursOfOperation
		*out = make([]HoursOfOperation, len(*in))
		copy(*out, *in)
	}
	if in.AutoClose != nil {
		in, out := &in.AutoClose, &out.AutoClose
		*out = new(AutoClose)
		**out = **in
	}
	if in.AutoResolve != nil {
		in, out := &in.AutoResolve, &out.AutoResolve
		*out = new(AutoClose)
		**out = **in
	}
	in.IncidentResponders.DeepCopyInto(&out.IncidentResponders)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentRuleSpec.
func (in *IncidentRuleSpec) DeepCopy() *IncidentRuleSpec {
	if in == nil {
		return nil
	}
	out := new(IncidentRuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IncidentTemplate) DeepCopyInto(out *IncidentTemplate) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IncidentTemplate.
func (in *IncidentTemplate) DeepCopy() *IncidentTemplate {
	if in == nil {
		return nil
	}
	out := new(IncidentTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NotificationConfig) DeepCopyInto(out *NotificationConfig) {
	*out = *in
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NotificationConfig.
func (in *NotificationConfig) DeepCopy() *NotificationConfig {
	if in == nil {
		return nil
	}
	out := new(NotificationConfig)
	in.DeepCopyInto(out)
	return out
}
