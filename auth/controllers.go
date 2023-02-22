package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	"github.com/labstack/echo/v4"
	oryclient "github.com/ory/client-go"
)

type InviteUserRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

func (k *KratosHandler) InviteUser(c echo.Context) error {
	var reqData InviteUserRequest
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPErrorMessage{Error: err.Error(), Message: "invalid request"})
	}

	identity, err := k.createUser(c.Request().Context(), reqData.FirstName, reqData.LastName, reqData.Email)
	if err != nil {
		// User already exists
		if strings.Contains(err.Error(), http.StatusText(http.StatusConflict)) {
			return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{Error: "user already exists", Message: "error creating user"})
		}

		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{Error: err.Error(), Message: "error creating user"})
	}

	link, err := k.createRecoveryLink(c.Request().Context(), identity.Id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{Error: err.Error(), Message: "error creating recovery link"})
	}

	body := fmt.Sprintf(inviteUserTemplate, reqData.FirstName, link)
	inviteMail := mail.New(reqData.Email, "User Invite", body, "text/html")
	if err = inviteMail.Send(); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{Error: err.Error(), Message: "error sending email"})
	}

	respJSON := []byte(fmt.Sprintf(`{"link": "%s"}`, link))
	return c.JSONBlob(http.StatusOK, respJSON)
}

func UpdateAccountState(c echo.Context) error {
	var reqData struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Invalid request body",
		})
	}

	if !oryclient.IdentityState(reqData.State).IsValid() {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   fmt.Sprintf("Invalid state: %s", reqData.State),
			Message: fmt.Sprintf("Invalid state. Allowed values are %s", oryclient.AllowedIdentityStateEnumValues),
		})
	}

	if err := db.UpdateIdentityState(reqData.ID, reqData.State); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error updating database",
		})
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "success"})
}

func UpdateAccountProperties(c echo.Context) error {
	var props api.PersonProperties
	if err := c.Bind(&props); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Invalid request body",
		})
	}

	err := db.UpdateUserProperties(c.Param("id"), props)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error updating database",
		})
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "success"})
}
