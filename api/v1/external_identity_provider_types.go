package v1

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/kopper"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultExternalIdentityMaxTokenAge = 5 * time.Minute

var (
	DefaultExternalIdentityAlgorithms = []string{"RS256", "ES256"}

	// SupportedExternalIdentityAlgorithms are asymmetric algorithms only:
	// a shared secret would let anyone who can verify a token also mint one.
	SupportedExternalIdentityAlgorithms = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//
// ExternalIdentityProvider lets an external application authenticate its users to Mission Control
// with JWTs signed by its own issuer.
//
// A user is represented by a federated person (keyed by <provider>:<username>), who may be mapped into
// the teams the provider allows. RoleBindings select them through those teams, or by <provider>:<username> in people.
type ExternalIdentityProvider struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   ExternalIdentityProviderSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status ExternalIdentityProviderStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

// +kubebuilder:object:generate=true
type ExternalIdentityProviderSpec struct {
	// Issuer must exactly match the iss claim of accepted tokens. It can't be changed.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="issuer can't be changed"
	// +kubebuilder:validation:Pattern=`^(https://.+|http://(localhost|127(\.[0-9]{1,3}){3}|\[::1\])(:[0-9]+)?([/?#].*)?)$`
	Issuer string `json:"issuer" yaml:"issuer"`

	// Audience must be one of the aud claims of accepted tokens, usually the Mission Control URL.
	// +kubebuilder:validation:MinLength=1
	Audience string `json:"audience" yaml:"audience"`

	// JWKSURL is where the issuer's signing keys are fetched from.
	// Defaults to the jwks_uri in the issuer's OpenID configuration.
	// +kubebuilder:validation:Pattern=`^(https://.+|http://(localhost|127(\.[0-9]{1,3}){3}|\[::1\])(:[0-9]+)?([/?#].*)?)$`
	JWKSURL string `json:"jwksURL,omitempty" yaml:"jwksURL,omitempty"`

	// Algorithms accepted for token signatures. Defaults to RS256 and ES256.
	// +kubebuilder:validation:items:Enum=RS256;RS384;RS512;PS256;PS384;PS512;ES256;ES384;ES512;EdDSA
	Algorithms []string `json:"algorithms,omitempty" yaml:"algorithms,omitempty"`

	// MaxTokenAge rejects tokens whose lifetime (exp - iat) is longer. Defaults to 5m.
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="maxTokenAge must be a positive duration"
	MaxTokenAge string `json:"maxTokenAge,omitempty" yaml:"maxTokenAge,omitempty"`

	// Identity says what person a token becomes, and which teams they're in.
	Identity ExternalIdentity `json:"identity" yaml:"identity"`

	// Disabled rejects every token from this provider.
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`
}

// +kubebuilder:object:generate=true
type ExternalIdentity struct {
	// Username is a CEL expression over claims that returns the string uniquely and stably identifying the user
	// in the external application. The person is keyed by <provider>:<username>.
	// +kubebuilder:validation:MinLength=1
	Username string `json:"username" yaml:"username"`

	// Name is a CEL expression over claims that returns the user's display name. Defaults to the username.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Email is a CEL expression over claims that returns the user's email.
	// It's only displayed and never used to match Mission Control users.
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Teams maps the user into existing teams. Without it, the provider adds no one to any team.
	Teams *ExternalIdentityTeams `json:"teams,omitempty" yaml:"teams,omitempty"`
}

// +kubebuilder:object:generate=true
type ExternalIdentityTeams struct {
	// Mapper is a CEL expression over claims that returns a list of team names.
	// +kubebuilder:validation:MinLength=1
	Mapper string `json:"mapper" yaml:"mapper"`

	// Allowed are the only teams the provider may add anyone to.
	// +kubebuilder:validation:MinItems=1
	Allowed []string `json:"allowed" yaml:"allowed"`
}

func (t ExternalIdentityProviderSpec) GetMaxTokenAge() (time.Duration, error) {
	if t.MaxTokenAge == "" {
		return DefaultExternalIdentityMaxTokenAge, nil
	}

	maxAge, err := time.ParseDuration(t.MaxTokenAge)
	if err != nil {
		return 0, fmt.Errorf("invalid maxTokenAge %q: %w", t.MaxTokenAge, err)
	} else if maxAge <= 0 {
		return 0, fmt.Errorf("maxTokenAge must be positive")
	}

	return maxAge, nil
}

func (t ExternalIdentityProviderSpec) GetAlgorithms() []string {
	if len(t.Algorithms) == 0 {
		return DefaultExternalIdentityAlgorithms
	}
	return t.Algorithms
}

func (t ExternalIdentityProviderSpec) Validate() error {
	if err := ValidateIssuerURL("issuer", t.Issuer); err != nil {
		return err
	}

	if t.JWKSURL != "" {
		if err := ValidateIssuerURL("jwksURL", t.JWKSURL); err != nil {
			return err
		}
	}

	if t.Audience == "" {
		return fmt.Errorf("audience is required")
	}

	for _, alg := range t.GetAlgorithms() {
		if !slices.Contains(SupportedExternalIdentityAlgorithms, alg) {
			return fmt.Errorf("algorithm %q is not supported. Must be one of %v", alg, SupportedExternalIdentityAlgorithms)
		}
	}

	if _, err := t.GetMaxTokenAge(); err != nil {
		return err
	}

	if _, err := t.Identity.Compile(); err != nil {
		return err
	}

	return nil
}

// CompiledIdentity holds the compiled expressions of an ExternalIdentity. Name, Email and Mapper are nil when unset.
// +kubebuilder:object:generate=false
type CompiledIdentity struct {
	Username, Name, Email, Mapper cel.Program
}

var claimsEnv = sync.OnceValues(func() (*cel.Env, error) {
	return cel.NewEnv(cel.Variable("claims", cel.MapType(cel.StringType, cel.DynType)), ext.Strings())
})

// Compile compiles the identity's expressions, and checks that a team mapper has an allowlist.
func (t ExternalIdentity) Compile() (CompiledIdentity, error) {
	var compiled CompiledIdentity
	if strings.TrimSpace(t.Username) == "" {
		return compiled, fmt.Errorf("identity.username is required")
	}

	var err error
	if compiled.Username, err = compileClaimsExpression("identity.username", t.Username, cel.StringType); err != nil {
		return compiled, err
	}
	if t.Name != "" {
		if compiled.Name, err = compileClaimsExpression("identity.name", t.Name, cel.StringType); err != nil {
			return compiled, err
		}
	}
	if t.Email != "" {
		if compiled.Email, err = compileClaimsExpression("identity.email", t.Email, cel.StringType); err != nil {
			return compiled, err
		}
	}

	if t.Teams != nil {
		if strings.TrimSpace(t.Teams.Mapper) == "" {
			return compiled, fmt.Errorf("identity.teams.mapper is required")
		} else if len(lo.Compact(t.Teams.Allowed)) == 0 {
			return compiled, fmt.Errorf("identity.teams.allowed must name at least one team")
		}
		if compiled.Mapper, err = compileClaimsExpression("identity.teams.mapper", t.Teams.Mapper, cel.ListType(cel.StringType)); err != nil {
			return compiled, err
		}
	}

	return compiled, nil
}

// compileClaimsExpression compiles a CEL expression over claims that returns the given type, or dyn.
func compileClaimsExpression(field, expression string, output *cel.Type) (cel.Program, error) {
	env, err := claimsEnv()
	if err != nil {
		return nil, err
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("%s: %w", field, issues.Err())
	}

	if !output.IsAssignableType(ast.OutputType()) && ast.OutputType() != cel.DynType {
		return nil, fmt.Errorf("%s must return %s, not %s", field, output, ast.OutputType())
	}

	return env.Program(ast)
}

// ValidateIssuerURL requires https, except for loopback hosts.
func ValidateIssuerURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s %q must be an absolute URL", field, raw)
	}

	if u.Scheme == "https" {
		return nil
	}

	if u.Scheme == "http" {
		host := u.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
	}

	return fmt.Errorf("%s %q must use https", field, raw)
}

// ConditionAllowedTeams is the condition of an ExternalIdentityProvider with a team mapper that reports
// whether every allowed team exists, and warns when the shipped admins team is allowed.
const ConditionAllowedTeams = "AllowedTeams"

type ExternalIdentityProviderStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

var _ kopper.StatusPatchGenerator = (*ExternalIdentityProvider)(nil)
var _ kopper.StatusConditioner = (*ExternalIdentityProvider)(nil)
var _ kopper.ObservedGenerationSetter = (*ExternalIdentityProvider)(nil)

func (t *ExternalIdentityProvider) SetObservedGeneration(generation int64) {
	t.Status.ObservedGeneration = generation
}

func (t *ExternalIdentityProvider) GetObservedGeneration() int64 {
	return t.Status.ObservedGeneration
}

func (t *ExternalIdentityProvider) GetStatusConditions() *[]metav1.Condition {
	return &t.Status.Conditions
}

func (t *ExternalIdentityProvider) GenerateStatusPatch(original runtime.Object) client.Patch {
	og, ok := original.(*ExternalIdentityProvider)
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
// ExternalIdentityProviderList contains a list of ExternalIdentityProvider
type ExternalIdentityProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalIdentityProvider `json:"items"`
}
