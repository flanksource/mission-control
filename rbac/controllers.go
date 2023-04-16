package rbac

import (
	"net/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

func UpdateRoleForUser(c echo.Context) error {
	userID := c.Param("id")
	reqData := struct {
		Roles []string `json:"roles"`
	}{}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   err.Error(),
			Message: "Invalid request body",
		})
	}
	// Check role validity

	// Add method for DB
	if _, err := Enforcer.AddRolesForUser(userID, reqData.Roles); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Message: "Role updated successfully",
		})
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Message: "Role updated successfully",
	})
}

func GetRolesForUser(c echo.Context) error {
	userID := c.Param("id")
	roles, err := Enforcer.GetRolesForUser(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Role updated successfully",
		})

	}
	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Payload: roles,
	})
}
