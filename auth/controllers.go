package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/mail"
	"github.com/labstack/echo/v4"
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
