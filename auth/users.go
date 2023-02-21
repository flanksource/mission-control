package auth

import (
	"context"
	"os"

	"github.com/flanksource/incident-commander/db"
	client "github.com/ory/client-go"
)

const (
	AdminName            = "Admin"
	AdminEmail           = "admin@local"
	DefaultAdminPassword = "admin"
)

func (k *KratosHandler) createUser(ctx context.Context, firstName, lastName, email string) (*client.Identity, error) {
	adminCreateIdentityBody := *client.NewCreateIdentityBody(
		"default",
		map[string]any{
			"email": email,
			"name": map[string]string{
				"first": firstName,
				"last":  lastName,
			},
		},
	)

	createdIdentity, _, err := k.adminClient.IdentityApi.CreateIdentity(ctx).CreateIdentityBody(adminCreateIdentityBody).Execute()
	return createdIdentity, err
}

func (k *KratosHandler) createRecoveryLink(ctx context.Context, id string) (string, error) {
	adminCreateSelfServiceRecoveryLinkBody := client.NewCreateRecoveryLinkForIdentityBody(id)
	resp, _, err := k.adminClient.IdentityApi.CreateRecoveryLinkForIdentity(ctx).CreateRecoveryLinkForIdentityBody(*adminCreateSelfServiceRecoveryLinkBody).Execute()
	if err != nil {
		return "", err
	}
	return resp.GetRecoveryLink(), nil
}

func (k *KratosHandler) createAdminIdentity(ctx context.Context) (string, error) {
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = DefaultAdminPassword
	}

	config := *client.NewIdentityWithCredentialsPasswordConfig()
	config.SetPassword(adminPassword)

	password := *client.NewIdentityWithCredentialsPassword()
	password.SetConfig(config)

	creds := *client.NewIdentityWithCredentials()
	creds.SetPassword(password)

	body := *client.NewCreateIdentityBody(
		"default",
		map[string]any{
			"email": AdminEmail,
			"name": map[string]string{
				"first": AdminName,
			},
		},
	)
	body.SetCredentials(creds)

	createdIdentity, _, err := k.adminClient.IdentityApi.CreateIdentity(ctx).CreateIdentityBody(body).Execute()
	if err != nil {
		return "", err
	}

	return createdIdentity.Id, nil
}

func (k *KratosHandler) CreateAdminUser(ctx context.Context) (string, error) {
	var id string
	tx := db.Gorm.Raw(`SELECT id FROM identities WHERE traits->>'email' = ?`, AdminEmail).Scan(&id)
	if tx.Error != nil {
		return "", tx.Error
	}

	if tx.RowsAffected == 0 {
		return k.createAdminIdentity(ctx)
	}

	return id, nil
}
