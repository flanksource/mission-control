package expression

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var prgCache = cache.New(1*time.Hour, 1*time.Hour)

type programCache struct {
	program *cel.Program
	err     error
}

// Eval evaluates the given expression.
// The expression should return a boolean value that's supported by strconv.ParseBool.
func Eval(expression string, celEnv map[string]any, allEnvVars []string) (bool, error) {
	if expression == "" {
		return true, nil
	}

	prg, err := getOrCompileCELProgram(expression, allEnvVars)
	if err != nil {
		return false, fmt.Errorf("failed to compile program: %w", err)
	}

	out, _, err := (*prg).Eval(celEnv)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate program: %w", err)
	}

	result, err := strconv.ParseBool(fmt.Sprint(out))
	if err != nil {
		return false, fmt.Errorf("program result is not of a supported boolean type: %w", err)
	}

	return result, nil
}

// getOrCompileCELProgram returns a cached or compiled cel.Program for the given cel expression.
func getOrCompileCELProgram(expression string, allEnvVars []string) (*cel.Program, error) {
	if prg, exists := prgCache.Get(expression); exists {
		val := prg.(*programCache)
		if val.err != nil {
			return nil, val.err
		}

		return val.program, nil
	}

	prg, err := compileCELProgram(expression, allEnvVars)
	if err != nil {
		prgCache.SetDefault(expression, &programCache{err: err})
		return nil, err
	}

	prgCache.SetDefault(expression, &programCache{program: prg})
	return prg, nil
}

func compileCELProgram(expression string, allEnvVars []string) (*cel.Program, error) {
	celOpts := make([]cel.EnvOption, len(allEnvVars))
	for i := range allEnvVars {
		celOpts[i] = cel.Variable(allEnvVars[i], cel.AnyType)
	}
	env, err := cel.NewEnv(celOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create cel environment: %w", err)
	}

	ast, iss := env.Compile(expression)
	if iss.Err() != nil {
		return nil, fmt.Errorf("failed to compile expression: %w", iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create program: %w", err)
	}

	return &prg, nil
}
