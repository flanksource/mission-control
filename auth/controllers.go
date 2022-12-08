package auth

import (
	"fmt"
	"net/http"
	"strings"

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
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "invalid request"}`, err))
		return c.JSONBlob(http.StatusBadRequest, errMsg)
	}
	identity, err := k.createUser(reqData.FirstName, reqData.LastName, reqData.Email)
	if err != nil {
		var errMsg []byte
		// User already exists
		if strings.Contains(err.Error(), http.StatusText(http.StatusConflict)) {
			errMsg = []byte(`{"error": "User already exists", "message": "error creating user"}`)
		} else {
			errMsg = []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating user"}`, err))
		}
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	link, err := k.createRecoveryLink(identity.Id)
	if err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating recovery link"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	body := fmt.Sprintf(inviteUserTemplate, reqData.FirstName, link)
	inviteMail := mail.New(reqData.Email, "User Invite", body, "text/html")
	if err = inviteMail.Send(); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error sending email"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	respJSON := []byte(fmt.Sprintf(`{"link": "%s"}`, link))
	return c.JSONBlob(http.StatusOK, respJSON)
}
