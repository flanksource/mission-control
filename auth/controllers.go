package auth

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	oryClient "github.com/ory/client-go"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/rbac"
)

func RegisterRoutes(e *echo.Echo) {
	// Cannot register this routes in auth/rbac package as it would create a cyclic import
	e.POST("/auth/:id/update_state", UpdateAccountState)
	e.POST("/auth/:id/properties", UpdateAccountProperties)
	e.GET("/auth/whoami", WhoAmI)
}

type InviteUserRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

func (k *KratosHandler) InviteUser(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData InviteUserRequest
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	identity, err := k.createUser(ctx, reqData.FirstName, reqData.LastName, reqData.Email)
	if err != nil {
		// User already exists
		if strings.Contains(err.Error(), http.StatusText(http.StatusConflict)) {
			return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
				Err:     "User already exists",
				Message: "Error creating user",
			})
		}

		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Error creating user",
		})
	}

	if reqData.Role != "" {
		if err := rbac.AddRoleForUser(identity.Id, reqData.Role); err != nil {
			ctx.Logger.Errorf("failed to add role to user: %v", err)
		}
	}

	recoveryCode, recoveryLink, err := k.createRecoveryLink(ctx, identity.Id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "error creating recovery link"})
	}

	body := fmt.Sprintf(inviteUserTemplate, reqData.FirstName, recoveryLink, recoveryCode)
	inviteMail := mail.New(reqData.Email, "User Invite", body, "text/html")
	if err = inviteMail.Send(); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Error sending email",
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"link": recoveryLink,
		"code": recoveryCode,
	})
}

func UpdateAccountState(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var reqData struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	if !oryClient.IdentityState(reqData.State).IsValid() {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     fmt.Sprintf("Invalid state: %s", reqData.State),
			Message: fmt.Sprintf("Invalid state. Allowed values are %s", oryClient.AllowedIdentityStateEnumValues),
		})
	}

	if err := db.UpdateIdentityState(ctx, reqData.ID, reqData.State); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Error updating database",
		})
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

func UpdateAccountProperties(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var props api.PersonProperties
	if err := c.Bind(&props); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	err := db.UpdateUserProperties(ctx, c.Param("id"), props)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Error updating database",
		})
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "success"})
}

func WhoAmI(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	user := ctx.User()
	if user == nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Message: "Error fetching user",
		})
	}

	hostname, _ := os.Hostname()

	roles, err := rbac.RolesForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("Error getting roles: %v", err)
	}
	permissions, err := rbac.PermsForUser(user.ID.String())
	if err != nil {
		ctx.Warnf("Error getting permissions: %v", err)
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{
		Message: "success",
		Payload: map[string]any{
			"user":        user,
			"roles":       roles,
			"permissions": permissions,
			"hostname":    hostname,
		},
	})
}
