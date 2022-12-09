package jobs

import (
	"fmt"
	"strconv"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"

	"github.com/flanksource/commons/logger"
	"github.com/google/cel-go/cel"
)

func EvaluateEvidenceScripts() {
	logger.Debugf("Evaluating evidence scripts")

	// Fetch all evidences of open incidents which have a script
	evidences := db.GetEvidenceScripts()
	for _, evidence := range evidences {
		output, err := evaluate(evidence)
		if err != nil {
			logger.Errorf("Error running evidence script: %v", err)
			if err = db.UpdateEvidenceScriptResult(evidence.ID, false, err.Error()); err != nil {
				logger.Errorf("Error persisting evidence script result: %v", err)
			}
			continue
		}

		var result string
		done, err := strconv.ParseBool(output)
		if err != nil {
			result = "Script should evaluate to a boolean value"
		}
		if err = db.UpdateEvidenceScriptResult(evidence.ID, done, result); err != nil {
			logger.Errorf("Error persisting evidence script result: %v", err)
		}
	}
}

func evaluate(evidence api.Evidence) (string, error) {
	env, err := cel.NewEnv(
		cel.Variable("config", cel.AnyType),
		cel.Variable("component", cel.AnyType),
	)
	if err != nil {
		return "", err
	}

	ast, iss := env.Compile(evidence.Script)
	if iss.Err() != nil {
		return "", iss.Err()
	}

	prg, err := env.Program(ast)
	if err != nil {
		return "", err
	}

	out, _, err := prg.Eval(map[string]any{
		"config":    evidence.Config.Config,
		"component": evidence.Component.AsMap(),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprint(out), nil
}
