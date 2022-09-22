package api

import (
	"fmt"
	"strings"
)

type ComponentSelector struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Selector  string            `json:"selector,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Types     Items             `json:"types,omitempty"`
}

func (c ComponentSelector) String() string {
	s := ""
	if c.Name != "" {
		s += " name=" + c.Name
	}
	if c.Namespace != "" {
		s += " namespace=" + c.Namespace
	}
	if c.Selector != "" {
		s += " " + c.Selector
	}
	if c.Labels != nil {
		s += fmt.Sprintf(" labels=%s", c.Labels)
	}
	if len(c.Types) > 0 {
		s += " types=" + c.Types.String()
	}
	return strings.TrimSpace(s)

}
