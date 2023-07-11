package utils

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var prgCache = cache.New(24*time.Hour, 1*time.Hour)

// EvalExpression evaluates the given expression into a boolean.
// The expression is expected to return a boolean value supported by strconv.ParseBool.
func EvalExpression(expression string, celEnv map[string]any) (bool, error) {
	if expression == "" {
		return true, nil
	}

	isValid, err := evaluateFilterExpression(expression, celEnv)
	if err != nil {
		return false, fmt.Errorf("error evaluating filter expression: %w", err)
	}

	return isValid, nil
}

func evaluateFilterExpression(expression string, env map[string]any) (bool, error) {
	prg, err := getOrCompileCELProgram(expression, MapKeys(env))
	if err != nil {
		return false, err
	}

	out, _, err := (*prg).Eval(env)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(fmt.Sprint(out))
}

// getOrCompileCELProgram returns a cached or compiled cel.Program for the given cel expression.
func getOrCompileCELProgram(expression string, fields []string) (*cel.Program, error) {
	if prg, exists := prgCache.Get(expression); exists {
		return prg.(*cel.Program), nil
	}

	celOpts := make([]cel.EnvOption, len(fields))
	for i := range fields {
		celOpts[i] = cel.Variable(fields[i], cel.AnyType)
	}
	env, err := cel.NewEnv(celOpts...)
	if err != nil {
		return nil, err
	}

	ast, iss := env.Compile(expression)
	if iss.Err() != nil {
		return nil, iss.Err()
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, err
	}

	prgCache.SetDefault(expression, &prg)
	return &prg, nil
}
