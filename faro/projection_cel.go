package main

import (
	"fmt"

	"github.com/flanksource/gomplate/v3"
	"github.com/google/cel-go/cel"
)

// Projections evaluate through gomplate, which is the expression stack the rest
// of the platform already uses: playbooks, notifications, canaries and views all
// compile against the same environment. faro used to build its own, so a
// projection author had base CEL and four local helpers while everyone else had
// strings, lists, math, regex, encoders and a fold macro. The helpers that were
// genuinely missing -- text, int, float, date -- now live in gomplate's funcs
// package, which leaves nothing here to declare.
//
// The activation is the one thing faro still owns: source, target, context and
// item, declared by gomplate from the keys of the map it is handed.

// compileProjectionExpression checks an expression before anything runs it.
//
// gomplate compiles and caches on first evaluation, which would report a
// malformed expression in the middle of applying a projection -- after a
// network query, and possibly after another projection has already written its
// target. Compiling here keeps `faro projection verify` able to reject a
// document without making a request, and the error carries its source position.
func compileProjectionExpression(expression string) (*gomplate.Template, error) {
	template := &gomplate.Template{Expression: expression}
	env, err := cel.NewEnv(gomplate.CompileEnvOptions(projectionActivation(nil, nil, nil, nil), *template)...)
	if err != nil {
		return nil, err
	}
	if _, issues := env.Compile(expression); issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return template, nil
}

func evalProjectionBool(template *gomplate.Template, activation map[string]any) (bool, error) {
	value, err := gomplate.RunExpression(activation, *template)
	if err != nil {
		return false, err
	}
	matched, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %T, expected bool", value)
	}
	return matched, nil
}

func evalProjectionValue(template *gomplate.Template, activation map[string]any) (any, error) {
	return gomplate.RunExpression(activation, *template)
}
