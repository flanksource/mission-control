package jobs

import (
	"fmt"
	"strconv"

	"github.com/flanksource/commons/logger"
	"github.com/google/cel-go/cel"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/db"
)

func EvaluateEvidenceScripts() {
	logger.Debugf("Evaluating evidence scripts")

	// Fetch all evidences of open incidents which have a script
	evidences := db.GetEvidenceScripts()
	var incidentIDs []uuid.UUID
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
		incidentIDs = append(incidentIDs, evidence.Hypothesis.IncidentID)
	}

	db.ReconcileIncidentStatus(incidentIDs)
}

func evaluate(evidence db.EvidenceScriptInput) (string, error) {
	prg, err := getCELPrgFromCache(evidence)
	if err != nil {
		return "", err
	}
	out, _, err := (*prg).Eval(map[string]any{
		"config":    evidence.ConfigItem.Config,
		"component": evidence.Component.AsMap(),
		"incident":  evidence.Hypothesis.Incident.AsMap(),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprint(out), nil
}

func getCELPrgFromCache(evidence db.EvidenceScriptInput) (*cel.Program, error) {
	if prg, exists := prgCache.Get(evidence.Script); exists {
		return prg.(*cel.Program), nil
	}
	env, err := cel.NewEnv(
		cel.Variable("config", cel.AnyType),
		cel.Variable("component", cel.AnyType),
		cel.Variable("incident", cel.AnyType),
	)
	if err != nil {
		return nil, err
	}

	ast, iss := env.Compile(evidence.Script)
	if iss.Err() != nil {
		return nil, iss.Err()
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, err
	}
	prgCache.SetDefault(evidence.Script, &prg)
	return &prg, nil
}
