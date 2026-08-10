package main

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/google/cel-go/cel"
)

const identityTypeSkip = ""

type compiledIdentityTypeRule struct {
	index        int
	identityType string
	when         cel.Program
}

func compileIdentityTypeRules(rules []ProjectionUserTypeRule) ([]compiledIdentityTypeRule, error) {
	if len(rules) == 0 {
		rules = defaultIdentityTypeRules()
	}
	env, err := newProjectionEnv()
	if err != nil {
		return nil, err
	}
	compiled := make([]compiledIdentityTypeRule, 0, len(rules))
	for index, rule := range rules {
		if err := rule.validate(index); err != nil {
			return nil, err
		}
		program, err := compileProjectionExpression(env, rule.When)
		if err != nil {
			return nil, fmt.Errorf("userTypes[%d].when: %w", index, err)
		}
		compiled = append(compiled, compiledIdentityTypeRule{index: index, identityType: rule.IdentityType, when: program})
	}
	return compiled, nil
}

func classifyIdentityType(rules []compiledIdentityTypeRule, user models.ExternalUser, provider string) (string, error) {
	email := ""
	if user.Email != nil {
		email = *user.Email
	}
	source := map[string]any{
		"user_type":         user.UserType,
		"identity_provider": provider,
		"name":              user.Name,
		"email":             email,
		"aliases":           []string(user.Aliases),
	}
	matched := make([]compiledIdentityTypeRule, 0, 1)
	for _, rule := range rules {
		ok, err := evalProjectionBool(rule.when, projectionActivation(source, map[string]any{}, map[string]any{}, nil))
		if err != nil {
			return "", fmt.Errorf("userTypes[%d].when: %w", rule.index, err)
		}
		if ok {
			matched = append(matched, rule)
		}
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("external user has unmapped user_type %q for provider %q", user.UserType, provider)
	}
	if len(matched) > 1 {
		indexes := make([]string, 0, len(matched))
		for _, rule := range matched {
			indexes = append(indexes, fmt.Sprint(rule.index))
		}
		return "", fmt.Errorf("external user user_type %q for provider %q matches multiple userTypes rules: %s", user.UserType, provider, strings.Join(indexes, ", "))
	}
	if matched[0].identityType == "skip" {
		return identityTypeSkip, nil
	}
	return matched[0].identityType, nil
}

func defaultIdentityTypeRules() []ProjectionUserTypeRule {
	return []ProjectionUserTypeRule{
		{When: `source.user_type == "Group"`, IdentityType: "skip"},
		{When: `source.user_type == "User" && source.identity_provider == "Kubernetes" && source.name.matches("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$")`, IdentityType: "person"},
		{When: `source.user_type == "User" && source.identity_provider == "Kubernetes" && !source.name.matches("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$")`, IdentityType: "workload_identity"},
		{When: `source.identity_provider != "Kubernetes" && source.user_type in ["User", "user"]`, IdentityType: "person"},
		{When: `source.user_type in ["Human", "human", "GitHub::User", "local"]`, IdentityType: "person"},
		{When: `source.user_type in ["ServiceAccount", "AWSService"]`, IdentityType: "workload_identity"},
	}
}
