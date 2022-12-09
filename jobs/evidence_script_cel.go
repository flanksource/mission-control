package jobs

import (
	"github.com/flanksource/incident-commander/db"

	"github.com/flanksource/commons/logger"
	"github.com/google/cel-go/cel"
)

func EvaluateEvidenceScripts() {
	//logger.Debugf("Evaluating evidence scripts")
	logger.Infof("Evaluating evidence scripts")

	// Fetch all evidences of open incidents which have a script
	evidences := db.GetEvidenceScripts()
	for _, evidence := range evidences {
		env, err := cel.NewEnv(
			cel.Variable("config", cel.AnyType),
			cel.Variable("component", cel.AnyType),
		)
		if err != nil {
			logger.Errorf("Error creating cel env: %v", err)
			// TODO: Log fail result ?
			continue
		}

		ast, iss := env.Compile(evidence.Script)
		if iss.Err() != nil {
			logger.Errorf("Error compiling cel script: %v", iss.Err())
			// TODO: Log fail result ?
			continue
		}

		prg, err := env.Program(ast)
		if err != nil {
			logger.Errorf("Error compiling cel script: %v", iss.Err())
			// TODO: Log fail result ?
			continue
		}

		out, _, err := prg.Eval(map[string]interface{}{
			"config":    evidence.Config.Config,
			"component": evidence.Component.AsMap(),
		})
		if err != nil {
			logger.Errorf("Error persisting team components: %v", err)
		}

		// Store out in ScriptResult
		_ = out
		logger.Infof("Result of script [%s]: %s", evidence.Script, out)

		// If script result evaluate to truthy, mark dod as true
	}
}
