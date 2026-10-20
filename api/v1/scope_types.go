package v1

import (
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScopeResourceRef selects resources by id, name or namespace.
type ScopeResourceRef struct {
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ID string `json:"id,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace,omitempty"`
}

// ScopePlaybookRef selects playbooks by id, name or namespace.
// +kubebuilder:validation:Type=object
// +kubebuilder:validation:MinProperties=1
type ScopePlaybookRef ScopeResourceRef

// ScopeViewRef selects views by id, name or namespace.
// +kubebuilder:validation:Type=object
// +kubebuilder:validation:MinProperties=1
type ScopeViewRef ScopeResourceRef

// ScopeConfigSelector selects configs by identity and ownership, never by current state.
// Configs have no namespace: select it with tagSelector.
// +kubebuilder:validation:MinProperties=1
type ScopeConfigSelector struct {
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ID string `json:"id,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Agent string `json:"agent,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Types []string `json:"types,omitempty"`
	// +kubebuilder:validation:MinLength=1
	TagSelector string `json:"tagSelector,omitempty"`
	// +kubebuilder:validation:MinLength=1
	LabelSelector string `json:"labelSelector,omitempty"`
}

// ScopeLabelledSelector selects components and checks.
// +kubebuilder:validation:MinProperties=1
type ScopeLabelledSelector struct {
	ScopeResourceRef `json:",inline"`
	// +kubebuilder:validation:MinLength=1
	Agent string `json:"agent,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Types []string `json:"types,omitempty"`
	// +kubebuilder:validation:MinLength=1
	LabelSelector string `json:"labelSelector,omitempty"`
}

// ScopeCanarySelector selects canaries by identity and ownership.
// +kubebuilder:validation:MinProperties=1
type ScopeCanarySelector struct {
	ScopeResourceRef `json:",inline"`
	// +kubebuilder:validation:MinLength=1
	Agent string `json:"agent,omitempty"`
	// +kubebuilder:validation:MinLength=1
	LabelSelector string `json:"labelSelector,omitempty"`
}

// ScopeScraperSelector selects config scrapers by identity and ownership. Scrapers have no tags or labels.
// +kubebuilder:validation:MinProperties=1
type ScopeScraperSelector struct {
	ScopeResourceRef `json:",inline"`
	// +kubebuilder:validation:MinLength=1
	Agent string `json:"agent,omitempty"`
}

// ScopeConnectionSelector selects connections by identity and type.
// +kubebuilder:validation:MinProperties=1
type ScopeConnectionSelector struct {
	ScopeResourceRef `json:",inline"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	Types []string `json:"types,omitempty"`
}

// Resource types a Scope can only select entirely (see specs/authorization/scopes.md, Section 5.3).
// duty's policy package doesn't define them yet.
const (
	ResourceApplication         = "application"
	ResourceNotification        = "notification"
	ResourceNotificationSilence = "notificationSilence"
	ResourceAgent               = "agent"
	ResourcePerson              = "person"
	ResourceTeam                = "team"
	ResourceScope               = "scope"
	ResourceRole                = "role"
	ResourceRoleBinding         = "roleBinding"
)

// WholeTypeResources are the resource types whose only selector is name: "*".
var WholeTypeResources = []string{
	ResourceApplication, ResourceNotification, ResourceNotificationSilence, ResourceAgent,
	ResourcePerson, ResourceTeam, ResourceScope, ResourceRole, ResourceRoleBinding,
}

// ScopeWholeTypeSelector selects every resource of a type that Scopes can only select entirely.
type ScopeWholeTypeSelector struct {
	// Name must be "*": partial selections of the type aren't supported yet.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum="*"
	Name string `json:"name"`
}

// ScopeTarget selects resources of exactly one type. Scope owns the authoring fields;
// duty's query options and state filters must not enter the authorization language.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="[has(self.config), has(self.component), has(self.check), has(self.playbook), has(self.canary), has(self.view), has(self.connection), has(self.scraper), has(self.application), has(self.notification), has(self.notificationSilence), has(self.agent), has(self.person), has(self.team), has(self.scope), has(self.role), has(self.roleBinding)].filter(x, x).size() == 1",message="exactly one of config, component, check, playbook, canary, view, connection, scraper, application, notification, notificationSilence, agent, person, team, scope, role, or roleBinding must be specified"
type ScopeTarget struct {
	Config     *ScopeConfigSelector     `json:"config,omitempty"`
	Component  *ScopeLabelledSelector   `json:"component,omitempty"`
	Check      *ScopeLabelledSelector   `json:"check,omitempty"`
	Playbook   *ScopePlaybookRef        `json:"playbook,omitempty"`
	Canary     *ScopeCanarySelector     `json:"canary,omitempty"`
	View       *ScopeViewRef            `json:"view,omitempty"`
	Connection *ScopeConnectionSelector `json:"connection,omitempty"`
	Scraper    *ScopeScraperSelector    `json:"scraper,omitempty"`

	Application         *ScopeWholeTypeSelector `json:"application,omitempty"`
	Notification        *ScopeWholeTypeSelector `json:"notification,omitempty"`
	NotificationSilence *ScopeWholeTypeSelector `json:"notificationSilence,omitempty"`
	Agent               *ScopeWholeTypeSelector `json:"agent,omitempty"`
	Person              *ScopeWholeTypeSelector `json:"person,omitempty"`
	Team                *ScopeWholeTypeSelector `json:"team,omitempty"`
	Scope               *ScopeWholeTypeSelector `json:"scope,omitempty"`
	Role                *ScopeWholeTypeSelector `json:"role,omitempty"`
	RoleBinding         *ScopeWholeTypeSelector `json:"roleBinding,omitempty"`
}

// wholeTypeSelectors returns the target's selector of each type in WholeTypeResources, nil when unset.
func (t ScopeTarget) wholeTypeSelectors() map[string]*ScopeWholeTypeSelector {
	return map[string]*ScopeWholeTypeSelector{
		ResourceApplication:         t.Application,
		ResourceNotification:        t.Notification,
		ResourceNotificationSilence: t.NotificationSilence,
		ResourceAgent:               t.Agent,
		ResourcePerson:              t.Person,
		ResourceTeam:                t.Team,
		ResourceScope:               t.Scope,
		ResourceRole:                t.Role,
		ResourceRoleBinding:         t.RoleBinding,
	}
}

// TypeCount returns how many resource types the target sets. A valid target sets exactly one.
func (t ScopeTarget) TypeCount() int {
	n := 0
	for _, set := range []bool{
		t.Config != nil, t.Component != nil, t.Check != nil, t.Canary != nil,
		t.Playbook != nil, t.View != nil, t.Connection != nil, t.Scraper != nil,
	} {
		if set {
			n++
		}
	}
	for _, selector := range t.wholeTypeSelectors() {
		if selector != nil {
			n++
		}
	}
	return n
}

// Selector converts only Scope's identity and ownership fields for duty's matcher.
func (t ScopeTarget) Selector() (string, types.ResourceSelector) {
	switch {
	case t.Config != nil:
		s := t.Config
		return policy.ResourceConfig, types.ResourceSelector{
			ID: s.ID, Name: s.Name,
			Agent: s.Agent, Types: s.Types, TagSelector: s.TagSelector, LabelSelector: s.LabelSelector,
		}

	case t.Component != nil:
		return policy.ResourceComponent, t.Component.selector()

	case t.Check != nil:
		return policy.ResourceCheck, t.Check.selector()

	case t.Playbook != nil:
		return policy.ResourcePlaybook, ScopeResourceRef(*t.Playbook).selector()

	case t.Canary != nil:
		s := t.Canary
		return policy.ResourceCanary, types.ResourceSelector{
			ID: s.ID, Name: s.Name, Namespace: s.Namespace, Agent: s.Agent, LabelSelector: s.LabelSelector,
		}

	case t.View != nil:
		return policy.ResourceView, ScopeResourceRef(*t.View).selector()

	case t.Connection != nil:
		s := t.Connection
		return policy.ResourceConnection, types.ResourceSelector{
			ID: s.ID, Name: s.Name, Namespace: s.Namespace, Types: s.Types,
		}

	case t.Scraper != nil:
		s := t.Scraper
		return policy.ResourceScraper, types.ResourceSelector{
			ID: s.ID, Name: s.Name, Namespace: s.Namespace, Agent: s.Agent,
		}
	}

	selectors := t.wholeTypeSelectors()
	for _, kind := range WholeTypeResources {
		if s := selectors[kind]; s != nil {
			return kind, types.ResourceSelector{Name: s.Name}
		}
	}
	return "", types.ResourceSelector{}
}

func (s ScopeResourceRef) selector() types.ResourceSelector {
	return types.ResourceSelector{ID: s.ID, Name: s.Name, Namespace: s.Namespace}
}

func (s ScopeLabelledSelector) selector() types.ResourceSelector {
	return types.ResourceSelector{
		ID: s.ID, Name: s.Name, Namespace: s.Namespace, Agent: s.Agent, Types: s.Types, LabelSelector: s.LabelSelector,
	}
}

// +kubebuilder:object:generate=true
type ScopeSpec struct {
	// Description provides a brief explanation of what this scope covers
	Description string `json:"description,omitempty"`

	// Targets select the resources of the Scope.
	// A Scope is the union of what its targets select: a resource belongs to the Scope
	// when a target of the resource's type matches it.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Targets []ScopeTarget `json:"targets"`
}

type ScopeStatus struct {
	// ObservedGeneration is the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Scope's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
//
// Scope selects resources, of one or more types, for access control
type Scope struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ScopeSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ScopeStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*Scope)(nil)
var _ kopper.StatusConditioner = (*Scope)(nil)
var _ kopper.ObservedGenerationSetter = (*Scope)(nil)

func (t *Scope) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *Scope) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *Scope) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *Scope) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*Scope)
	if !ok {
		return nil
	}

	if cmp.Diff(t.Status, og.Status) == "" {
		return nil
	}

	clientObj, ok := original.(client.Object)
	if !ok {
		return nil
	}

	return client.MergeFrom(clientObj)
}

// +kubebuilder:object:root=true
//
// ScopeList contains a list of Scope
type ScopeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Scope `json:"items"`
}
