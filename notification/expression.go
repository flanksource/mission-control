package notification

import (
	"fmt"
	"strconv"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/logs"
	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var (
	prgCache = cache.New(1*time.Hour, 1*time.Hour)

	// Stores the whether the previous expression successed or failed
	expressionResultCache = cache.New(1*time.Hour, 1*time.Hour)

	allEnvVars = []string{"check", "canary", "incident", "team", "responder", "comment", "evidence", "hypothesis"}
)

type programCache struct {
	program *cel.Program
	err     error
}

type ExpressionRunner struct {
	ResourceID   string
	ResourceType string
	CelEnv       map[string]any
}

// Eval evaluates the given expression into a boolean.
// The expression should return a boolean value that's supported by strconv.ParseBool.
func (t ExpressionRunner) Eval(ctx api.Context, expression string) (bool, error) {
	jobHistory := models.NewJobHistory("NotificationFilterEval", t.ResourceType, t.ResourceID).Start()
	defer func() {
		passingCurrently := jobHistory.ErrorCount == 0

		if value, found := expressionResultCache.Get(t.ResourceID); found {
			if passingPreviously, ok := value.(bool); ok {
				if passingPreviously == passingCurrently {
					// to avoid excessive db calls, we only save the job history if the expression evaluation changes
					return
				}
			}
		}

		logs.IfError(db.PersistJobHistory(ctx, jobHistory.End()), "error persisting notification filter evaluation job history")
		expressionResultCache.SetDefault(t.ResourceID, passingCurrently)
	}()

	result, err := t.eval(ctx, expression)
	if err != nil {
		jobHistory.AddError(err.Error())
		return false, err
	}

	jobHistory.IncrSuccess()
	return result, nil
}

func (t ExpressionRunner) eval(ctx api.Context, expression string) (bool, error) {
	if expression == "" {
		return true, nil
	}

	prg, err := t.getOrCompileCELProgram(ctx, expression)
	if err != nil {
		return false, fmt.Errorf("failed to compile program: %w", err)
	}

	out, _, err := (*prg).Eval(t.CelEnv)
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
func (t ExpressionRunner) getOrCompileCELProgram(ctx api.Context, expression string) (*cel.Program, error) {
	if prg, exists := prgCache.Get(expression); exists {
		val := prg.(*programCache)
		if val.err != nil {
			return nil, val.err
		}

		return val.program, nil
	}

	prg, err := compileCELProgram(ctx, expression)
	if err != nil {
		prgCache.SetDefault(expression, &programCache{err: err})
		return nil, err
	}

	prgCache.SetDefault(expression, &programCache{program: prg})
	return prg, nil
}

func compileCELProgram(ctx api.Context, expression string) (*cel.Program, error) {
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
