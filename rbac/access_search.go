package rbac

import (
	"slices"
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// searchableTypes are the resource types a subject access search lists.
var searchableTypes = []string{policy.ResourcePlaybook, policy.ResourceView, policy.ResourceConnection}

type SubjectAccessSearchRequest struct {
	Subject string `json:"subject"`
	Action  string `json:"action"`

	// ResourceTypes are the types to search: playbook, view and connection. Every one the action is performed on when
	// empty.
	ResourceTypes []string `json:"resource_types,omitempty"`

	// Target is the resource to check an action that takes a target on, e.g. the config to run playbooks on.
	// Without one, only rules that allow the action without a target match.
	Target SubjectAccessReviewResource `json:"target,omitempty"`
}

type SubjectAccessSearchResult struct {
	ResourceType string `json:"resource_type"`
	ID           string `json:"id"`
}

type SubjectAccessSearchResponse struct {
	Subject       string                      `json:"subject"`
	Action        string                      `json:"action"`
	ResourceTypes []string                    `json:"resource_types"`
	Target        SubjectAccessReviewResource `json:"target"`

	// Requirements are the other checks the operation makes, e.g. read on the target. When the subject fails one, the
	// operation is refused on every resource, so there are no results.
	Requirements []ReviewRequirement `json:"requirements,omitempty"`

	Total   int                         `json:"total"`
	Results []SubjectAccessSearchResult `json:"results"`
}

func (req *SubjectAccessSearchRequest) validate() (adapter.ActionContract, error) {
	req.Subject = strings.TrimSpace(req.Subject)
	if req.Subject == "" {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "subject is required")
	} else if !reviewableSubject(req.Subject) {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "%q isn't a subject", req.Subject)
	}

	if req.Action == "" {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "action is required")
	}
	contract, err := adapter.ContractFor(req.Action)
	if err != nil {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "%v", err)
	}

	if len(req.ResourceTypes) == 0 {
		req.ResourceTypes = lo.Intersect(searchableTypes, contract.Resources)
		if len(req.ResourceTypes) == 0 {
			return adapter.ActionContract{}, api.Errorf(api.EINVALID, "%s isn't performed on a %s", req.Action, strings.Join(searchableTypes, ", "))
		}
	}

	var types []string
	for _, kind := range req.ResourceTypes {
		kind = strings.ToLower(strings.TrimSpace(kind))
		switch {
		case !slices.Contains(searchableTypes, kind):
			return adapter.ActionContract{}, api.Errorf(api.EINVALID, "unsupported resource_type %q, only %s are supported", kind, strings.Join(searchableTypes, ", "))
		case !slices.Contains(contract.Resources, kind):
			return adapter.ActionContract{}, api.Errorf(api.EINVALID, "%s isn't performed on a %s", req.Action, kind)
		case !slices.Contains(types, kind):
			types = append(types, kind)
		}
	}
	req.ResourceTypes = types

	if target := req.Target.ids(); len(target) > 1 || req.Target.Global != "" {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "target names one config, component or check")
	} else if len(target) == 1 && !slices.Contains(contract.Targets, lo.Keys(target)[0]) {
		return adapter.ActionContract{}, api.Errorf(api.EINVALID, "%s doesn't take a %s target", req.Action, lo.Keys(target)[0])
	}

	return contract, nil
}

// RunSubjectAccessSearch lists the resources the subject may perform the action on, of those the reviewer may read
// (specs/authorization/roles.md, Section 2.3). It decides each as a review does.
func RunSubjectAccessSearch(ctx context.Context, req SubjectAccessSearchRequest) (SubjectAccessSearchResponse, error) {
	contract, err := req.validate()
	if err != nil {
		return SubjectAccessSearchResponse{}, err
	}

	targetIDs := req.Target.ids()
	target, err := loadReviewResources(ctx, targetIDs)
	if err != nil {
		return SubjectAccessSearchResponse{}, err
	}

	response := SubjectAccessSearchResponse{
		Subject:       req.Subject,
		Action:        req.Action,
		ResourceTypes: req.ResourceTypes,
		Target:        req.Target,
		Results:       make([]SubjectAccessSearchResult, 0),
	}

	snapshot, err := dutyRBAC.SnapshotPolicy()
	if err != nil {
		return SubjectAccessSearchResponse{}, ctx.Oops().Wrap(err)
	}

	// The resources to decide, each with the target
	candidates := map[string][]*models.ABACAttribute{}
	var all []*models.ABACAttribute
	for _, kind := range req.ResourceTypes {
		attrs, err := readableResources(ctx, kind)
		if err != nil {
			return SubjectAccessSearchResponse{}, err
		}
		for _, attr := range attrs {
			attr.Config, attr.Component, attr.Check = target.Config, target.Component, target.Check
		}
		candidates[kind] = attrs
		all = append(all, attrs...)
	}

	// The requirements and every resource are checked against the same moment of the Scope membership, as an
	// operation checks its action and requirements
	checkCtx, err := membership.ForOperation(ctx, dutyRBAC.MembershipRefs(append(all, target)...)...)
	if err != nil {
		return SubjectAccessSearchResponse{}, ctx.Oops().Wrapf(err, "failed to read the scope membership")
	}
	decide := func(object any) (bool, error) {
		explanation, err := snapshot.Explain(checkCtx, req.Subject, object, req.Action)
		if err != nil {
			return false, ctx.Oops().Wrapf(err, "failed to review %s for %s", req.Action, req.Subject)
		}
		return explanation.Allowed, nil
	}

	// What the operation also checks doesn't depend on the resource, e.g. read on the target or mcp:use
	check := reviewCheck{action: req.Action, contract: contract, attr: target}
	if len(targetIDs) == 1 {
		kind := lo.Keys(targetIDs)[0]
		check.target = &membership.Ref{Type: kind, ID: uuid.MustParse(targetIDs[kind])}
	}
	if response.Requirements, err = reviewRequirements(checkCtx, snapshot, req.Subject, check); err != nil {
		return SubjectAccessSearchResponse{}, err
	}
	if lo.SomeBy(response.Requirements, func(r ReviewRequirement) bool { return !r.Allowed }) {
		return response, nil
	}

	for _, kind := range req.ResourceTypes {
		attrs := candidates[kind]
		if checkedOnType(contract, kind) {
			if len(attrs) == 0 {
				continue
			}
			if allowed, err := decide(adapter.ObjectForType(kind)); err != nil {
				return SubjectAccessSearchResponse{}, err
			} else if allowed {
				for _, attr := range attrs {
					response.Results = append(response.Results, SubjectAccessSearchResult{ResourceType: kind, ID: resourceID(kind, attr).String()})
				}
			}
			continue
		}

		for _, attr := range attrs {
			if allowed, err := decide(attr); err != nil {
				return SubjectAccessSearchResponse{}, err
			} else if allowed {
				response.Results = append(response.Results, SubjectAccessSearchResult{ResourceType: kind, ID: resourceID(kind, attr).String()})
			}
		}
	}

	response.Total = len(response.Results)
	return response, nil
}

// readableResources lists the resources of the type the reviewer may read, sorted by name.
func readableResources(ctx context.Context, kind string) ([]*models.ABACAttribute, error) {
	var attrs []*models.ABACAttribute
	switch kind {
	case policy.ResourcePlaybook:
		var playbooks []models.Playbook
		if err := ctx.DB().Where("deleted_at IS NULL").Order("COALESCE(title, name), id").Find(&playbooks).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to list playbooks")
		}
		attrs = lo.Map(playbooks, func(p models.Playbook, _ int) *models.ABACAttribute { return &models.ABACAttribute{Playbook: p} })

	case policy.ResourceView:
		var views []models.View
		if err := ctx.DB().Where("deleted_at IS NULL").Order("name, id").Find(&views).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to list views")
		}
		attrs = lo.Map(views, func(v models.View, _ int) *models.ABACAttribute { return &models.ABACAttribute{View: v} })

	case policy.ResourceConnection:
		// Connections are read as a whole type (specs/authorization/roles.md, Section 2.2)
		if !CheckContext(ctx, policy.ObjectConnection, policy.ActionRead) {
			return nil, nil
		}
		var connections []models.Connection
		if err := ctx.DB().Where("deleted_at IS NULL").Order("name, id").Find(&connections).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "failed to list connections")
		}
		return lo.Map(connections, func(c models.Connection, _ int) *models.ABACAttribute { return &models.ABACAttribute{Connection: c} }), nil
	}

	readCtx, err := membership.ForOperation(ctx, dutyRBAC.MembershipRefs(attrs...)...)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to read the scope membership of %s", kind)
	}
	return lo.Filter(attrs, func(attr *models.ABACAttribute, _ int) bool {
		return HasPermission(readCtx, readCtx.Subject(), attr, policy.ActionRead)
	}), nil
}

func resourceID(kind string, attr *models.ABACAttribute) uuid.UUID {
	switch kind {
	case policy.ResourcePlaybook:
		return attr.Playbook.ID
	case policy.ResourceView:
		return attr.View.ID
	case policy.ResourceConnection:
		return attr.Connection.ID
	}
	return uuid.Nil
}
