package v1

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Permission struct {
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
	Team string `json:"team,omitempty" yaml:"team,omitempty"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

const (
	PlaybookParameterTypeCheck     = "check"
	PlaybookParameterTypeCheckbox  = "checkbox"
	PlaybookParameterTypeCode      = "code"
	PlaybookParameterTypeComponent = "component"
	PlaybookParameterTypeConfig    = "config"
	PlaybookParameterTypeList      = "list"
	PlaybookParameterTypePeople    = "people"
	PlaybookParameterTypeTeam      = "team"
	PlaybookParameterTypeText      = "text"
	PlaybookParameterTypeMillis    = "Millis"
	PlaybookParameterTypeBytes     = "Bytes"
)

// PlaybookParameter defines a parameter that a playbook needs to run.
type PlaybookParameter struct {
	// Name is the key for this parameter.
	// It's used to address the parameter on templates.
	Name string `json:"name" yaml:"name"`
	// Specify the default value of the parameter.
	Default dutyTypes.GoTemplate `json:"default,omitempty" yaml:"default,omitempty" template:"true"`
	// Label shown on the UI
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Icon        string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// +kubebuilder:validation:Enum=check;checkbox;code;component;config;list;people;team;text;bytes;millicores
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Properties json.RawMessage `json:"properties,omitempty" yaml:"properties,omitempty"`
}

type PlaybookApprovers struct {
	// Emails of the approvers
	People []string `json:"people,omitempty" yaml:"people,omitempty"`

	// Names of the teams
	Teams []string `json:"teams,omitempty" yaml:"teams,omitempty"`
}

func (t *PlaybookApprovers) Empty() bool {
	return len(t.People) == 0 && len(t.Teams) == 0
}

type PlaybookApprovalType string

const (
	// PlaybookApprovalTypeAny means just a single approval can suffice.
	PlaybookApprovalTypeAny PlaybookApprovalType = "any"

	// PlaybookApprovalTypeAll means all approvals are required
	PlaybookApprovalTypeAll PlaybookApprovalType = "all"
)

type PlaybookApproval struct {
	Type      PlaybookApprovalType `json:"type,omitempty" yaml:"type,omitempty"`
	Approvers PlaybookApprovers    `json:"approvers,omitempty" yaml:"approvers,omitempty"`
}

type PlaybookTriggerEvent struct {
	// Event to listen for.
	Event string `json:"event" yaml:"event"`

	// Labels specifies the key-value pairs that the associated event's resource must match.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// CEL expression for additional event filtering.
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`
}

type PlaybookEventWebhookAuthBasic struct {
	Username dutyTypes.EnvVar `json:"username" yaml:"username"`
	Password dutyTypes.EnvVar `json:"password" yaml:"password"`
}

type PlaybookEventWebhookAuthGithub struct {
	// Token is the secret token for the webhook.
	//  Doc: https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
	Token dutyTypes.EnvVar `json:"token" yaml:"token"`
}

type PlaybookEventWebhookAuthSVIX struct {
	// Secret is the webhook signing secret
	Secret dutyTypes.EnvVar `json:"secret" yaml:"secret"`
	// TimestampTolerance specifies the tolerance for the timestamp verification.
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h", "d", "w", "y".
	TimestampTolerance string `json:"verifyTimestamp,omitempty" yaml:"verifyTimestamp,omitempty"`
}

type PlaybookEventWebhookAuthJWT struct {
	JWKSURI string `json:"jwksUri" yaml:"jwksUri"`
}

type PlaybookEventWebhookAuth struct {
	Basic  *PlaybookEventWebhookAuthBasic  `json:"basic,omitempty" yaml:"basic,omitempty"`
	Github *PlaybookEventWebhookAuthGithub `json:"github,omitempty" yaml:"github,omitempty"`
	SVIX   *PlaybookEventWebhookAuthSVIX   `json:"svix,omitempty" yaml:"svix,omitempty"`
	JWT    *PlaybookEventWebhookAuthJWT    `json:"jwt,omitempty" yaml:"jwt,omitempty"`
}

type PlaybookTriggerWebhook struct {
	Path           string                    `json:"path" yaml:"path"`
	Authentication *PlaybookEventWebhookAuth `json:"authentication,omitempty" yaml:"authentication,omitempty"`
}

type PlaybookTriggerEvents struct {
	Canary    []PlaybookTriggerEvent `json:"canary,omitempty" yaml:"canary,omitempty"`
	Config    []PlaybookTriggerEvent `json:"config,omitempty" yaml:"config,omitempty"`
	Component []PlaybookTriggerEvent `json:"component,omitempty" yaml:"component,omitempty"`
}

// PlaybookTrigger defines the list of supported events & to trigger a playbook.
type PlaybookTrigger struct {
	PlaybookTriggerEvents `json:",inline" yaml:",inline"`

	// Webhook creates a new endpoint that triggers this playbook
	Webhook *PlaybookTriggerWebhook `json:"webhook,omitempty" yaml:"webhook,omitempty"`
}

type PlaybookSpec struct {
	Title string `json:"title,omitempty" yaml:"title,omitempty"`

	// Short description of the playbook.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	Category string `json:"category,omitempty" yaml:"category,omitempty"`

	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`

	// `On` defines triggers that will automatically trigger the playbook.
	// If multiple events are defined, only one of those events needs to occur to trigger the playbook.
	// If multiple triggering events occur at the same time, multiple playbook runs will be triggered.
	On *PlaybookTrigger `json:"on,omitempty" yaml:"on,omitempty"`

	// RunsOn specifies the agents that can run this playbook.
	// When left empty, the playbook will run on the main instance itself.
	RunsOn []string `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`

	// Env is a list of env vars that are templatable and accessible in templates.
	// Env vars are similar to playbook parameters except they do not get
	// persisted and are meant to be used for confidential information.
	Env []dutyTypes.EnvVar `json:"env,omitempty" yaml:"env,omitempty"`

	// TemplatesOn specifies where the templating happens.
	// Available options:
	//  - host
	//  - agent
	// When left empty, the templating is done on the main instance(host) itself.
	TemplatesOn string `json:"templatesOn,omitempty" yaml:"templatesOn,omitempty"`

	// Permissions ...
	Permissions []Permission `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// Configs filters what config items can run on this playbook.
	Configs dutyTypes.ResourceSelectors `json:"configs,omitempty" yaml:"configs,omitempty"`

	// Checks filters what checks can run on this playbook.
	Checks dutyTypes.ResourceSelectors `json:"checks,omitempty" yaml:"checks,omitempty"`

	// Components what components can run on this playbook.
	Components dutyTypes.ResourceSelectors `json:"components,omitempty" yaml:"components,omitempty"`

	// Define and document what parameters are required to run this playbook.
	Parameters []PlaybookParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	// List of actions that need to be executed by this playbook.
	Actions []PlaybookAction `json:"actions" yaml:"actions"`

	// CEL Expressions that check if a playbook should be executed
	Filters []string `json:"filters,omitempty" yaml:"filters,omitempty"`

	// Approval defines the individuals and teams authorized to approve runs of this playbook.
	Approval *PlaybookApproval `json:"approval,omitempty" yaml:"approval,omitempty"`
}

// PlaybookStatus defines the observed state of Playbook
type PlaybookStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Playbook is the schema for the Playbooks API
type Playbook struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   PlaybookSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status PlaybookStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

func PlaybookFromModel(p models.Playbook) (Playbook, error) {
	var spec PlaybookSpec
	if err := json.Unmarshal(p.Spec, &spec); err != nil {
		return Playbook{}, err
	}

	out := Playbook{
		ObjectMeta: metav1.ObjectMeta{
			Name:              p.Name,
			UID:               types.UID(p.ID.String()),
			CreationTimestamp: metav1.Time{Time: p.CreatedAt},
			Namespace:         p.Namespace,
		},
		Spec: spec,
	}

	return out, nil
}

func (p Playbook) ToModel() (*models.Playbook, error) {
	var id uuid.UUID
	if v, err := uuid.Parse(string(p.GetUID())); err == nil {
		id = v
	}

	specJSON, err := json.Marshal(p.Spec)
	if err != nil {
		return nil, err
	}

	return &models.Playbook{
		ID:          id,
		Name:        p.Name,
		Title:       lo.CoalesceOrEmpty(p.Spec.Title, p.Name),
		Namespace:   p.Namespace,
		Description: p.Spec.Description,
		Icon:        p.Spec.Icon,
		Spec:        specJSON,
		Category:    p.Spec.Category,
	}, nil
}

// +kubebuilder:object:root=true

// PlaybookList contains a list of Playbook
type PlaybookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Playbook `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Playbook{}, &PlaybookList{})
}
