package main

import (
	"fmt"

	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
)

func newProjectionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("source", cel.DynType),
		cel.Variable("target", cel.DynType),
		cel.Variable("context", cel.DynType),
		cel.Variable("item", cel.DynType),
		cel.Function("text", cel.Overload(
			"projection_text_dyn",
			[]*cel.Type{cel.DynType},
			cel.StringType,
			cel.UnaryBinding(projectionText),
		)),
	)
}

func projectionText(value ref.Val) ref.Val {
	if value == celtypes.NullValue || value.Value() == nil {
		return celtypes.NewErr("text() does not accept null")
	}
	return celtypes.String(fmt.Sprint(value.Value()))
}

func compileProjectionExpression(env *cel.Env, expression string) (cel.Program, error) {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

func evalProjectionBool(program cel.Program, activation map[string]any) (bool, error) {
	value, _, err := program.Eval(activation)
	if err != nil {
		return false, err
	}
	matched, ok := value.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, expected bool", value.Value())
	}
	return matched, nil
}

func evalProjectionValue(program cel.Program, activation map[string]any) (any, error) {
	value, _, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	return projectionNativeValue(value)
}

func projectionNativeValue(value ref.Val) (any, error) {
	if value == celtypes.NullValue || value.Value() == nil {
		return nil, nil
	}
	native, err := value.ConvertToNative(celtypes.JSONValueType)
	if err != nil {
		return nil, err
	}
	jsonValue, ok := native.(*structpb.Value)
	if !ok {
		return nil, fmt.Errorf("CEL value converted to %T, expected JSON value", native)
	}
	return jsonValue.AsInterface(), nil
}
