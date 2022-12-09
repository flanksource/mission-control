package auth

import (
	"context"

	client "github.com/ory/client-go"
)

func (k *KratosHandler) createUser(firstName, lastName, email string) (*client.Identity, error) {
	adminCreateIdentityBody := *client.NewAdminCreateIdentityBody(
		"default",
		map[string]any{
			"email": email,
			"name": map[string]string{
				"first": firstName,
				"last":  lastName,
			},
		},
	)

	createdIdentity, _, err := k.adminClient.V0alpha2Api.AdminCreateIdentity(context.Background()).AdminCreateIdentityBody(adminCreateIdentityBody).Execute()
	return createdIdentity, err
}

func (k *KratosHandler) createRecoveryLink(id string) (string, error) {
	adminCreateSelfServiceRecoveryLinkBody := *client.NewAdminCreateSelfServiceRecoveryLinkBody(id)
	resp, _, err := k.adminClient.V0alpha2Api.AdminCreateSelfServiceRecoveryLink(context.Background()).AdminCreateSelfServiceRecoveryLinkBody(adminCreateSelfServiceRecoveryLinkBody).Execute()
	if err != nil {
		return "", err
	}
	return resp.GetRecoveryLink(), nil
}
