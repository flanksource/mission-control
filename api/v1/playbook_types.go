package v1

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/config/schemas"
	"github.com/xeipuuv/gojsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var playbookSchemaLoader = gojsonschema.NewBytesLoader(schemas.PlaybookSpecSchemaLoader)

type Permission struct {
	Role string `json:"role,omitempty" yaml:"role,omitempty"`
	Team string `json:"team,omitempty" yaml:"team,omitempty"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

// PlaybookResourceFilter defines a filter that decides whether a resource (config or a component)
// is permitted be run on the Playbook.
type PlaybookResourceFilter struct {
	Type string            `json:"type,omitempty" yaml:"type,omitempty"`
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
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
)

// PlaybookParameter defines a parameter that a playbook needs to run.
type PlaybookParameter struct {
	// Name is the key for this parameter.
	// It's used to address the parameter on templates.
	Name string `json:"name" yaml:"name"`
	// Specify the default value of the parameter. It is templatable.
	Default string `json:"default,omitempty" yaml:"default,omitempty" template:"true"`
	// Label shown on the UI
	Label       string `json:"label" yaml:"label"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Icon        string `json:"icon,omitempty" yaml:"icon,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// +kubebuilder:validation:Enum=check;checkbox;code;component;config;list;people;team;text
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
	// Short description of the playbook.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`

	// `On` defines triggers that will automatically trigger the playbook.
	// If multiple events are defined, only one of those events needs to occur to trigger the playbook.
	// If multiple triggering events occur at the same time, multiple playbook runs will be triggered.
	On *PlaybookTrigger `json:"on,omitempty" yaml:"on,omitempty"`

	// RunsOn specifies the agents that can run this playbook.
	// When left empty, the playbook will run on the main instance itself.
	RunsOn []string `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`

	// TemplatesOn specifies where the templating happens.
	// Available options:
	//  - host
	//  - agent
	// When left empty, the templating is done on the main instance(host) itself.
	TemplatesOn string `json:"templatesOn,omitempty" yaml:"templatesOn,omitempty"`

	// Permissions ...
	Permissions []Permission `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// Configs filters what config items can run on this playbook.
	Configs []PlaybookResourceFilter `json:"configs,omitempty" yaml:"configs,omitempty"`

	// Checks filters what checks can run on this playbook.
	Checks []PlaybookResourceFilter `json:"checks,omitempty" yaml:"checks,omitempty"`

	// Components what components can run on this playbook.
	Components []PlaybookResourceFilter `json:"components,omitempty" yaml:"components,omitempty"`

	// Define and document what parameters are required to run this playbook.
	Parameters []PlaybookParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	// List of actions that need to be executed by this playbook.
	Actions []PlaybookAction `json:"actions" yaml:"actions"`

	// Approval defines the individuals and teams authorized to approve runs of this playbook.
	Approval *PlaybookApproval `json:"approval,omitempty" yaml:"approval,omitempty"`
}

func ValidatePlaybookSpec(schema []byte) (error, error) {
	documentLoader := gojsonschema.NewBytesLoader(schema)
	result, err := gojsonschema.Validate(playbookSchemaLoader, documentLoader)
	if err != nil {
		return nil, err
	}

	if len(result.Errors()) != 0 {
		return fmt.Errorf("spec is invalid: %v", result.Errors()), nil
	}

	return nil, nil
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
		return Playbook{}, nil
	}

	out := Playbook{
		ObjectMeta: metav1.ObjectMeta{
			Name:              p.Name,
			UID:               types.UID(p.ID.String()),
			CreationTimestamp: metav1.Time{Time: p.CreatedAt},
		},
		Spec: spec,
	}

	return out, nil
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
