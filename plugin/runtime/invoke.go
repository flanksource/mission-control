package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/plugin"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
)

const MaxInvokeDepth = 5

type Invoker func(ctx dutyContext.Context, pluginID uuid.UUID, req *pluginpb.InvokeRequest) (*pluginpb.InvokeResponse, error)

type Request struct {
	Context      context.Context
	PluginRef    string
	Operation    string
	ParamsJSON   []byte
	ConfigItemID string
	Caller       *pluginpb.CallerContext
	User         *models.Person
	Subject      string
	Roles        []string
	Depth        int
	Deadline     *timestamppb.Timestamp
	Timeout      time.Duration
}

func Invoke(ctx dutyContext.Context, req Request, invoker Invoker) (*pluginpb.InvokeResponse, *registry.Entry, error) {
	if invoker == nil {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EINTERNAL, "plugin invocation is not configured")
	}
	if req.PluginRef == "" {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EINVALID, "plugin is required")
	}
	if req.Operation == "" {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EINVALID, "operation is required")
	}
	if req.User == nil {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "not logged in")
	}
	if req.Depth > MaxInvokeDepth {
		return nil, nil, dutyAPI.Errorf(dutyAPI.EINVALID, "maximum plugin invocation depth %d exceeded", MaxInvokeDepth)
	}

	entry, err := ResolvePlugin(ctx, req.PluginRef)
	if err != nil {
		return nil, nil, err
	}
	if OperationDef(entry, req.Operation) == nil {
		return nil, entry, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "plugin %q operation %q not found", req.PluginRef, req.Operation)
	}
	if req.ConfigItemID != "" {
		matches, err := SelectorMatches(ctx, entry, req.ConfigItemID)
		if err != nil {
			return nil, entry, err
		}
		if !matches {
			return nil, entry, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "plugin %q is not enabled for config %s", req.PluginRef, req.ConfigItemID)
		}
	}

	subject := req.Subject
	if subject == "" {
		subject = req.User.ID.String()
	}
	if err := EnforceInvokePermission(ctx, subject, entry, req.Operation, req.ConfigItemID); err != nil {
		return nil, entry, err
	}

	token, err := auth.MintPluginInvocationTokenWithDepth(*req.User, entry.ID, req.Depth, req.Roles...)
	if err != nil {
		return nil, entry, ctx.Oops().Wrapf(err, "mint plugin invocation token")
	}

	invokeCtx := ctx
	if req.Context != nil {
		if dutyCtx, ok := req.Context.(dutyContext.Context); ok {
			invokeCtx = dutyCtx
		} else {
			invokeCtx = ctx.Wrap(req.Context)
		}
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		invokeCtx, cancel = invokeCtx.WithTimeout(req.Timeout)
		defer cancel()
	}
	invokeCtx = invokeCtx.Wrap(metadata.AppendToOutgoingContext(invokeCtx, plugin.InvocationTokenGRPCMetadataKey, token)).WithUser(req.User)

	caller := req.Caller
	if caller == nil {
		caller = CallerFromUser(req.User)
	}
	resp, err := invoker(invokeCtx, entry.ID, &pluginpb.InvokeRequest{
		Operation:    req.Operation,
		ParamsJson:   req.ParamsJSON,
		ConfigItemId: req.ConfigItemID,
		Caller:       caller,
		Deadline:     req.Deadline,
	})
	return resp, entry, err
}

func ResolvePlugin(ctx dutyContext.Context, ref string) (*registry.Entry, error) {
	entry, err := registry.Default.Resolve(ref)
	if err != nil {
		if errors.Is(err, registry.ErrAmbiguousPlugin) {
			return nil, ctx.Oops().Code(dutyAPI.ECONFLICT).Wrap(err)
		}
		return nil, ctx.Oops().Wrap(err)
	}
	if entry == nil || entry.Manifest == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %q not running", ref)
	}
	return entry, nil
}

func OperationDef(entry *registry.Entry, op string) *pluginpb.OperationDef {
	if entry == nil || entry.Manifest == nil {
		return nil
	}
	for _, def := range entry.Manifest.Operations {
		if def != nil && def.Name == op {
			return def
		}
	}
	return nil
}

func SelectorMatches(ctx dutyContext.Context, entry *registry.Entry, configID string) (bool, error) {
	if entry == nil {
		return false, nil
	}
	selector := entry.Spec.Selector
	if selector.IsEmpty() {
		return true, nil
	}

	item, err := query.ConfigItemFromCache(ctx, configID)
	if err != nil {
		return false, err
	}

	return selector.Matches(item)
}

func EnforceInvokePermission(ctx dutyContext.Context, subject string, entry *registry.Entry, op, configID string) error {
	if subject == "" {
		return ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("not logged in")
	}

	attr := &models.ABACAttribute{}
	if configID != "" {
		item, err := query.ConfigItemFromCache(ctx, configID)
		if err != nil {
			return ctx.Oops().Wrapf(err, "get config item %s", configID)
		}
		attr.Config = item
	}

	if CanInvokePluginOperation(ctx, subject, attr, entry.Name, op) {
		return nil
	}
	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("not allowed to invoke plugin %s operation %s", entry.Name, op)
}

func CanInvokePluginOperation(ctx dutyContext.Context, subject string, attr *models.ABACAttribute, pluginName, op string) bool {
	return dutyRBAC.HasPermission(ctx, subject, attr, policy.NewPluginInvokeAction(pluginName, op))
}

func CallerFromUser(user *models.Person) *pluginpb.CallerContext {
	cc := &pluginpb.CallerContext{}
	if user != nil {
		cc.UserId = user.ID.String()
	}
	return cc
}

func PluginSubject(namespace, name string) string {
	return fmt.Sprintf("plugin:%s/%s", namespace, name)
}
