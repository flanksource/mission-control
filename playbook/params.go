package playbook

import (
	"encoding/json"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/secret"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"gopkg.in/yaml.v2"
)

// Parameters supplied to a playbook run
type PlaybookRuntimeParameters map[string]string

func (p *PlaybookRuntimeParameters) Sanitize(ctx context.Context, specs []v1.PlaybookParameter) error {
	if p == nil {
		return nil
	}

	secretParameters := make(map[string]struct{})
	for _, spec := range specs {
		if spec.Type == v1.PlaybookParameterTypeSecret {
			secretParameters[spec.Name] = struct{}{}
		}
	}

	for key, v := range *p {
		if _, ok := secretParameters[key]; ok {
			ciphertext, err := secret.Encrypt(ctx, secret.Sensitive(v))
			if err != nil {
				return oops.Wrapf(err, "failed to encrypt secret parameter %s", key)
			}

			(*p)[key] = ciphertext.String()
		}
	}

	return nil
}

type RunParams struct {
	ID          uuid.UUID                 `yaml:"id,omitempty" json:"id,omitempty"`
	AgentID     *uuid.UUID                `yaml:"agent_id,omitempty" json:"agent_id,omitempty"`
	ConfigID    *uuid.UUID                `yaml:"config_id,omitempty" json:"config_id,omitempty"`
	CheckID     *uuid.UUID                `yaml:"check_id,omitempty" json:"check_id,omitempty"`
	ComponentID *uuid.UUID                `yaml:"component_id,omitempty" json:"component_id,omitempty"`
	Params      PlaybookRuntimeParameters `yaml:"params,omitempty" json:"params,omitempty"`
	Request     *actions.WebhookRequest   `yaml:"request,omitempty" json:"request,omitempty"`

	NotificationSendID *uuid.UUID `yaml:"notification_send_id,omitempty" json:"notification_send_id,omitempty"`
	ParentID           *uuid.UUID `yaml:"parent_id,omitempty" json:"parent_id,omitempty"`
}

func (p *RunParams) String() string {
	b, err := yaml.Marshal(p)
	if err != nil {
		return ""
	}

	return string(b)
}

func (p *RunParams) Context() map[string]any {
	m := make(map[string]any)
	b, _ := json.Marshal(&p)
	if err := json.Unmarshal(b, &m); err != nil {
		return m
	}
	return nil
}

func (r *RunParams) valid() error {
	if r.ID == uuid.Nil {
		return oops.Errorf("playbook id is required")
	}

	var providedCount int
	if r.ConfigID != nil {
		providedCount++
	}
	if r.ComponentID != nil {
		providedCount++
	}
	if r.CheckID != nil {
		providedCount++
	}

	if providedCount > 1 {
		return oops.Errorf("provide none or exactly one of config_id, component_id, or check_id")
	}

	return nil
}

func (r *RunParams) setDefaults(ctx context.Context, spec v1.PlaybookSpec, templateEnv actions.TemplateEnv) error {
	if len(spec.Parameters) == len(r.Params) {
		return nil
	}

	defaultParams := []v1.PlaybookParameter{}
	for _, p := range spec.Parameters {
		if _, ok := r.Params[p.Name]; !ok {
			defaultParams = append(defaultParams, p)
		}
	}

	templater := ctx.NewStructTemplater(templateEnv.AsMap(ctx), "template", nil)
	if err := templater.Walk(&defaultParams); err != nil {
		return ctx.Oops().Wrap(err)
	}

	if r.Params == nil {
		r.Params = make(map[string]string)
	}
	for i := range defaultParams {
		r.Params[defaultParams[i].Name] = string(defaultParams[i].Default)
	}
	return nil
}

func (r *RunParams) validateParams(params []v1.PlaybookParameter) error {
	all := lo.Map(params, func(v v1.PlaybookParameter, _ int) string { return v.Name })
	required := lo.Map(lo.Filter(params, func(v v1.PlaybookParameter, _ int) bool {
		return v.Required
	}), func(v v1.PlaybookParameter, _ int) string { return v.Name })

	var missing []string
	for _, p := range required {
		if v, ok := r.Params[p]; !ok || lo.IsEmpty(v) {
			missing = append(missing, p)
		}
	}

	if len(missing) != 0 {
		return oops.Errorf("missing required parameter(s): %s", strings.Join(missing, ","))
	}

	unknownParams, _ := lo.Difference(
		lo.MapToSlice(r.Params, func(k string, _ string) string { return k }),
		lo.Map(params, func(v v1.PlaybookParameter, _ int) string { return v.Name }),
	)

	if len(unknownParams) != 0 {
		return oops.Errorf("unknown parameter(s): %s, valid parameters are: %s", strings.Join(unknownParams, ", "), strings.Join(all, ", "))
	}

	return nil
}
