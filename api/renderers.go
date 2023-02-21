package api

import "fmt"

type Renderers struct {
	Components []RenderComponent `json:"components,omitempty"`
	Properties []RenderComponent `json:"properties,omitempty"`
}

type RenderComponent struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	JSX  string `json:"jsx,omitempty"`
}

func (c *RenderComponent) Key(isProp bool) string {
	prefix := "component"
	if isProp {
		prefix = "property"
	}

	if c.Type != "" {
		return fmt.Sprintf("%s_%s_%s", prefix, c.Type, c.Name)
	}

	return fmt.Sprintf("%s_%s", prefix, c.Name)
}
