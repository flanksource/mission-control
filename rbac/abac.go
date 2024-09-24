package rbac

import (
	"github.com/flanksource/duty/models"
	"github.com/labstack/echo/v4"
)

type ABACResource struct {
	Playbook   models.Playbook   `json:"playbook"`
	Config     models.ConfigItem `json:"config"`
	Connection models.Connection `json:"connection"`
	Component  models.Component  `json:"component"`
}

func (r ABACResource) AsMap() map[string]any {
	return map[string]any{
		"component":  r.Component.AsMap(),
		"config":     r.Config.AsMap(),
		"connection": r.Connection.AsMap(),
		"playbook":   r.Playbook.AsMap(),
	}
}

type EchoABACResourceGetter func(c echo.Context, action string) (string, *ABACResource, error)
