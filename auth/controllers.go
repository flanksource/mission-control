package auth

import (
	"fmt"
	"net/http"

	"github.com/flanksource/incident-commander/utils"
	"github.com/labstack/echo/v4"
)

type InviteUserRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
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
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating user"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	link, err := k.createRecoveryLink(identity.Id)
	if err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating recovery link"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	body := fmt.Sprintf(inviteUserTemplate, reqData.FirstName, link)
	mail := utils.NewMail(reqData.Email, "User Invite", body, "text/html")
	if err = utils.SendMail(mail); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error sending email"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	respJSON := []byte(fmt.Sprintf(`{"link": "%s"}`, link))
	return c.JSONBlob(http.StatusOK, respJSON)
}
