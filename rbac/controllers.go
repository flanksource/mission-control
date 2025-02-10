package rbac

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"
)

func UpdateRoleForUser(c echo.Context) error {
	userID := c.Param("id")
	reqData := struct {
		Roles []string `json:"roles"`
	}{}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	if err := rbac.AddRoleForUser(userID, reqData.Roles...); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error updating roles",
		})
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Message: "Role updated successfully",
	})
}

func GetRolesForUser(c echo.Context) error {
	userID := c.Param("id")
	roles, err := rbac.RolesForUser(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error getting roles",
		})
	}
	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Payload: roles,
	})
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
