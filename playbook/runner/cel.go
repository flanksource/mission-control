package runner

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/playbook/actions"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/samber/lo"
)

const (

	// skip the action
	actionFilterSkip = "skip"

	// run the action if any of the previous actions timed out
	actionFilterTimeout = "timeout"
)

func getActionCelEnvs(ctx context.Context, env actions.TemplateEnv) []cel.EnvOption {
	return []cel.EnvOption{

		cel.Function("success",
			cel.Overload("success",
				nil,
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					var statuses []models.PlaybookActionStatus
					err := ctx.DB().Select("status").Model(&models.PlaybookRunAction{}).
						Where("playbook_run_id = ?", env.Run.ID).Find(&statuses).Error
					if err != nil {
						return types.WrapErr(err)
					}

					return types.Bool(len(lo.Filter(statuses, func(i models.PlaybookActionStatus, _ int) bool {
						return i == models.PlaybookActionStatusFailed
					})) == 0)
				}),
			),
		),

		cel.Function("skip",
			cel.Overload("skip",
				nil,
				cel.StringType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.String(actionFilterSkip)
				}),
			),
		),

		cel.Function("failure",
			cel.Overload("failure",
				nil,
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					var statuses []models.PlaybookActionStatus
					err := ctx.DB().Select("status").Model(&models.PlaybookRunAction{}).
						Where("playbook_run_id = ?", env.Run.ID).Find(&statuses).Error
					if err != nil {
						return types.WrapErr(err)
					}

					return types.Bool(len(lo.Filter(statuses, func(i models.PlaybookActionStatus, _ int) bool {
						return i == models.PlaybookActionStatusFailed
					})) > 0)
				}),
			),
		),
		cel.Function("always",
			cel.Overload("always",
				nil,
				cel.BoolType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.Bool(true)
				}),
			),
		),

		cel.Function("getLastAction",
			cel.Overload("getLastAction",
				[]*cel.Type{},
				cel.MapType(cel.StringType, cel.DynType),
				cel.FunctionBinding(func(value ...ref.Val) ref.Val {
					r, err := GetLastAction(ctx, env.Run.ID.String(), env.Action.ID.String())
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
					r, err := GetActionByName(ctx, env.Run.ID.String(), value.Value().(string))
					if err != nil {
						return types.WrapErr(err)
					}

					return types.DefaultTypeAdapter.NativeToValue(r)
				}),
			),
		),
	}
}
