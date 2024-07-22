package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	oryclient "github.com/ory/client-go"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
)

type InviteUserRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

func (k *KratosHandler) InviteUser(c echo.Context) error {
	var reqData InviteUserRequest
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	identity, err := k.createUser(c.Request().Context(), reqData.FirstName, reqData.LastName, reqData.Email)
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

	link, err := k.createRecoveryLink(c.Request().Context(), identity.Id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "error creating recovery link"})
	}

	body := fmt.Sprintf(inviteUserTemplate, reqData.FirstName, link)
	inviteMail := mail.New(reqData.Email, "User Invite", body, "text/html")
	if err = inviteMail.Send(); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Error sending email",
		})
	}

	respJSON := []byte(fmt.Sprintf(`{"link": "%s"}`, link))
	return c.JSONBlob(http.StatusOK, respJSON)
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

	if !oryclient.IdentityState(reqData.State).IsValid() {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     fmt.Sprintf("Invalid state: %s", reqData.State),
			Message: fmt.Sprintf("Invalid state. Allowed values are %s", oryclient.AllowedIdentityStateEnumValues),
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
	var dbName string
	if dbURL, err := url.Parse(db.ConnectionString); err == nil {
		dbName = strings.TrimPrefix(dbURL.Path, "/")
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{
		Message: "success",
		Payload: map[string]any{
			"user":     user,
			"hostname": hostname,
			"database": dbName,
		},
	})
}
