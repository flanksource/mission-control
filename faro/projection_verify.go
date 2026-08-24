package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

func verifyProjection(projection Projection) error {
	if _, err := compileProjection(projection); err != nil {
		return fmt.Errorf("projection %s: %w", projection.Metadata.Name, err)
	}
	if query := projection.Spec.Source.Query.IdentityAccess; query != nil {
		if _, err := compileIdentityTypeRules(query.UserTypes); err != nil {
			return fmt.Errorf("projection %s spec.source.query.identityAccess: %w", projection.Metadata.Name, err)
		}
	}
	if projection.Spec.Target == nil {
		return nil
	}
	targetPath := projection.resolvePath(projection.Spec.Target.Path)
	body, err := os.ReadFile(targetPath)
	if err != nil {
		return err
	}
	_, _, entries, err := projectionTarget(body, projection.Spec.Target.Select)
	if err != nil {
		return fmt.Errorf("projection %s target: %w", projection.Metadata.Name, err)
	}
	// An http source addresses each entry by templating its fields, so a URL naming
	// a field the register does not carry is checkable without a single request —
	// which is the whole point of verify. Left to apply time it would surface as
	// every entry failing at once, against a live API.
	if query := projection.Spec.Source.Query.HTTP; query != nil {
		if err := verifyProjectionHTTPURL(*query, entries); err != nil {
			return fmt.Errorf("projection %s spec.source.query.http.url: %w", projection.Metadata.Name, err)
		}
	}
	entryPath := strings.TrimSuffix(projection.Spec.Target.Select, "[*]") + "[0]"
	for path := range projection.Spec.Set {
		if _, err := yaml.PathString(entryPath + strings.TrimPrefix(path, "$")); err != nil {
			return fmt.Errorf("projection %s spec.set key %q is not writable YAMLPath: %w", projection.Metadata.Name, path, projectionYAMLError(err))
		}
	}
	return validateProjectionTarget(projection, body)
}
