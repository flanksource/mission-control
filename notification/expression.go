package notification

import (
	"fmt"
	"strconv"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var (
	prgCache = cache.New(1*time.Hour, 1*time.Hour)

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

func (t ExpressionRunner) logToJobHistory(ctx *api.Context, name, errMsg string) {
	jobHistory := models.NewJobHistory(name, t.ResourceType, t.ResourceID)
	jobHistory.Start()
	jobHistory.AddError(errMsg)
	if err := db.PersistJobHistory(ctx, jobHistory.End()); err != nil {
		logger.Errorf("error persisting job history: %v", err)
	}
}

// Eval evaluates the given expression into a boolean.
// The expression should return a boolean value that's supported by strconv.ParseBool.
func (t ExpressionRunner) Eval(ctx *api.Context, expression string) (bool, error) {
	if expression == "" {
		return true, nil
	}

	prg, err := t.GetOrCompileCELProgram(ctx, expression)
	if err != nil {
		return false, err
	}

	out, _, err := (*prg).Eval(t.CelEnv)
	if err != nil {
		t.logToJobHistory(ctx, "NotificationFilterEval", fmt.Sprintf("%s: %s", expression, err.Error()))
		return false, err
	}

	return strconv.ParseBool(fmt.Sprint(out))
}

// GetOrCompileCELProgram returns a cached or compiled cel.Program for the given cel expression.
func (t ExpressionRunner) GetOrCompileCELProgram(ctx *api.Context, expression string) (*cel.Program, error) {
	if prg, exists := prgCache.Get(expression); exists {
		val := prg.(*programCache)
		if val.err != nil {
			return nil, val.err
		}

		return val.program, nil
	}

	var cachedData programCache
	defer func() {
		prgCache.SetDefault(expression, &cachedData)
		if cachedData.err != nil {
			t.logToJobHistory(ctx, "NotificationFilterCompile", fmt.Sprintf("%s: %s", expression, cachedData.err.Error()))
		}
	}()

	celOpts := make([]cel.EnvOption, len(allEnvVars))
	for i := range allEnvVars {
		celOpts[i] = cel.Variable(allEnvVars[i], cel.AnyType)
	}
	env, err := cel.NewEnv(celOpts...)
	if err != nil {
		cachedData.err = err
		return nil, err
	}

	ast, iss := env.Compile(expression)
	if iss.Err() != nil {
		cachedData.err = iss.Err()
		return nil, iss.Err()
	}

	prg, err := env.Program(ast)
	if err != nil {
		cachedData.err = err
		return nil, err
	}

	cachedData.program = &prg
	return &prg, nil
}
