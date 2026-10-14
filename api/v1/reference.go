package v1

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// ParseReference returns the namespace and name a reference to a Scope or Role names: `name`, in the namespace of
// the object it's written in, or `namespace/name` (specs/authorization/overview.md, "References").
func ParseReference(namespace, ref string) (string, string, error) {
	refNamespace, name, qualified := strings.Cut(ref, "/")
	if !qualified {
		refNamespace, name = namespace, ref
	} else if errs := validation.IsDNS1123Label(refNamespace); len(errs) > 0 {
		return "", "", fmt.Errorf("%q: namespace %q is invalid: %s", ref, refNamespace, strings.Join(errs, ", "))
	}

	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return "", "", fmt.Errorf("%q: name %q is invalid: %s", ref, name, strings.Join(errs, ", "))
	}

	return refNamespace, name, nil
}

// validateReference checks a reference on its own, without the namespace it's resolved in.
func validateReference(ref string) error {
	_, _, err := ParseReference("default", ref)
	return err
}
