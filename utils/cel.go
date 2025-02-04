package utils

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/duty/models"
	dutyTypes "github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3/conv"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

var CelFunctions = []cel.EnvOption{cel.Function("matchQuery",
	cel.Overload("matchQuery",
		[]*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType},
		cel.BoolType,
		cel.FunctionBinding(func(args ...ref.Val) ref.Val {
			resourceSelectableRaw, err := convertMap(args[0])
			if err != nil {
				return types.WrapErr(errors.New("matchQuery expects the first argument to be a map[string]any"))
			}

			peg := conv.ToString(args[1])

			// TODO: need to decide what struct to unmarshal to
			// Or, we could implemnet
			// type ResourceSelectableMap map[string]any
			var config models.ConfigItem
			if b, err := json.Marshal(resourceSelectableRaw); err != nil {
				return types.WrapErr(err)
			} else if err := json.Unmarshal(b, &config); err != nil {
				return types.WrapErr(err)
			}

			rs := dutyTypes.ResourceSelector{Search: peg}
			return types.Bool(rs.Matches(config))
		}),
	),
)}

func convertMap(arg ref.Val) (map[string]any, error) {
	switch m := arg.Value().(type) {
	case map[ref.Val]ref.Val:
		var out = make(map[string]any)
		for key, val := range m {
			out[key.Value().(string)] = val.Value()
		}
		return out, nil
	case map[string]any:
		return m, nil
	default:
		return nil, fmt.Errorf("not a map %T", arg.Value())
	}
}
