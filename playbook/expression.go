package playbook

import (
	"github.com/flanksource/duty/context"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	// always run the action
	actionFilterAlways = "always"

	// skip the action
	actionFilterSkip = "skip"

	// run the action if any of the previous actions failed
	actionFilterFailure = "failure"

	// run the action if any of the previous actions timed out
	actionFilterTimeout = "timeout"

	// run the action if all of the previous actions succeeded
	actionFilterSuccess = "success"
)

// Functions for filters in playbook actions.
// Available in go template & cel expressions.
var actionFilterFuncs = map[string]any{
	"always":  func() any { return actionFilterAlways },
	"failure": func() any { return actionFilterFailure },
	"skip":    func() any { return actionFilterSkip },
	"success": func() any { return actionFilterSuccess },
	"timeout": func() any { return actionFilterTimeout },
}

func getActionCelEnvs(ctx context.Context, runID, callerActionID string) []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("getLastAction",
			cel.Overload("getLastAction",
				[]*cel.Type{},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(value ...ref.Val) ref.Val {
					r, err := GetLastAction(ctx, runID, callerActionID)
					if err != nil {
						return types.WrapErr(err)
					}

					return types.DefaultTypeAdapter.NativeToValue(r)
				}),
			),
		),
		cel.Function("getAction",
			cel.Overload("getAction",
				[]*cel.Type{cel.StringType},
				cel.MapType(cel.StringType, cel.DynType),
				cel.UnaryBinding(func(value ref.Val) ref.Val {
					r, err := GetActionByName(ctx, runID, value.Value().(string))
					if err != nil {
						return types.WrapErr(err)
					}

					return types.DefaultTypeAdapter.NativeToValue(r)
				}),
			),
		),
	}
}
