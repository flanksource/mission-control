package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/duty/models"
)

var errMissingServiceAccountNamespace = errors.New("kubernetes ServiceAccount subject has no namespace")

func isMissingServiceAccountNamespace(err error) bool {
	return errors.Is(err, errMissingServiceAccountNamespace)
}

func canonicalWorkloadPrincipal(user models.ExternalUser, provider string) (string, error) {
	if provider != "Kubernetes" || user.UserType != "ServiceAccount" {
		if name := strings.TrimSpace(user.Name); name != "" {
			return name, nil
		}
		return "", fmt.Errorf("%s workload principal name is empty", provider)
	}

	principals := map[string]struct{}{}
	missingNamespace := false
	for _, alias := range user.Aliases {
		parts := strings.Split(alias, "/")
		if len(parts) != 5 || !strings.EqualFold(parts[0], "kubernetes") || !strings.EqualFold(parts[2], "serviceaccount") {
			continue
		}
		if strings.TrimSpace(parts[3]) == "" {
			missingNamespace = true
			continue
		}
		if strings.TrimSpace(parts[4]) == "" {
			return "", fmt.Errorf("kubernetes ServiceAccount alias %q has no name", alias)
		}
		principals["system:serviceaccount:"+parts[3]+":"+parts[4]] = struct{}{}
	}
	if len(principals) == 1 {
		for principal := range principals {
			return principal, nil
		}
	}
	if len(principals) > 1 {
		values := make([]string, 0, len(principals))
		for principal := range principals {
			values = append(values, principal)
		}
		sort.Strings(values)
		return "", fmt.Errorf("kubernetes ServiceAccount aliases resolve to multiple principals: %s", strings.Join(values, ", "))
	}
	if missingNamespace {
		return "", fmt.Errorf("%w for external user %s (%s)", errMissingServiceAccountNamespace, user.ID, strings.Join(user.Aliases, ", "))
	}
	return "", fmt.Errorf("kubernetes ServiceAccount %s has no canonical Kubernetes/<cluster>/ServiceAccount/<namespace>/<name> alias", user.ID)
}
