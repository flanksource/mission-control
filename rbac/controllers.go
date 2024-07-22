package rbac

import (
	"net/http"

	"github.com/flanksource/duty/api"
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

	if err := AddRoleForUser(userID, reqData.Roles...); err != nil {
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
	roles, err := RolesForUser(userID)
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
	return c.JSON(http.StatusOK, NewPermissions(enforcer.GetPolicy()))
}
