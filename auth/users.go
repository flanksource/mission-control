package auth

import (
	"context"

	"github.com/flanksource/incident-commander/db"
	client "github.com/ory/client-go"
)

const (
	AdminName     = "Admin"
	AdminEmail    = "admin@local"
	AdminPassword = "admin"
)

func (k *KratosHandler) createUser(firstName, lastName, email string) (*client.Identity, error) {
	adminCreateIdentityBody := *client.NewAdminCreateIdentityBody(
		"default",
		map[string]interface{}{
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

func (k *KratosHandler) createAdminIdentity() (string, error) {
	config := *client.NewAdminCreateIdentityImportCredentialsPasswordConfig()
	config.SetPassword(AdminPassword)

	password := *client.NewAdminCreateIdentityImportCredentialsPassword()
	password.SetConfig(config)

	creds := *client.NewAdminIdentityImportCredentials()
	creds.SetPassword(password)

	body := *client.NewAdminCreateIdentityBody(
		"default",
		map[string]any{
			"email": AdminEmail,
			"name": map[string]string{
				"first": AdminName,
			},
		},
	)
	body.SetCredentials(creds)

	createdIdentity, _, err := k.adminClient.V0alpha2Api.AdminCreateIdentity(context.Background()).AdminCreateIdentityBody(body).Execute()
	if err != nil {
		return "", err
	}
	return createdIdentity.Id, nil
}

func (k *KratosHandler) CreateAdminUser() (string, error) {
	var id string
	tx := db.Gorm.Raw(`SELECT id FROM identities WHERE traits->>'email' = ?`, AdminEmail).Scan(&id)
	if tx.Error != nil {
		return "", tx.Error
	}

	if tx.RowsAffected == 0 {
		return k.createAdminIdentity()
	}
	return id, nil
}
