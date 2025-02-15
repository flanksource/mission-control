package auth

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	client "github.com/ory/client-go"
)

var (
	KratosAPI, KratosAdminAPI string
)

type KratosHandler struct {
	client      *client.APIClient
	adminClient *client.APIClient
}

func NewKratosHandler() *KratosHandler {
	return &KratosHandler{
		client:      NewAPIClient(KratosAPI),
		adminClient: newAdminAPIClient(KratosAdminAPI),
	}
}

func NewAPIClient(kratosAPI string) *client.APIClient {
	return newKratosClient(kratosAPI)
}

func newAdminAPIClient(kratosAdminAPI string) *client.APIClient {
	return newKratosClient(kratosAdminAPI)
}

func newKratosClient(apiURL string) *client.APIClient {
	configuration := client.NewConfiguration()
	configuration.Servers = []client.ServerConfiguration{{URL: apiURL}}
	return client.NewAPIClient(configuration)
}

func (k *KratosHandler) createUser(ctx gocontext.Context, firstName, lastName, email string) (*client.Identity, error) {
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

func (k *KratosHandler) createRecoveryLink(ctx gocontext.Context, id string) (string, string, error) {
	createRecoveryCodeForIdentityBody := client.NewCreateRecoveryCodeForIdentityBody(id)
	resp, _, err := k.adminClient.IdentityApi.
		CreateRecoveryCodeForIdentity(ctx).
		CreateRecoveryCodeForIdentityBody(*createRecoveryCodeForIdentityBody).
		Execute()
	if err != nil {
		return "", "", err
	}

	return resp.GetRecoveryCode(), resp.GetRecoveryLink(), nil
}

func (k *KratosHandler) getIdentityCredentials(password string) client.IdentityWithCredentials {
	config := *client.NewIdentityWithCredentialsPasswordConfig()
	config.SetPassword(password)

	passwordBody := *client.NewIdentityWithCredentialsPassword()
	passwordBody.SetConfig(config)

	creds := *client.NewIdentityWithCredentials()
	creds.SetPassword(passwordBody)
	return creds
}

func (k *KratosHandler) createAdminIdentity(ctx gocontext.Context) (string, error) {
	body := *client.NewCreateIdentityBody(
		"default",
		map[string]any{
			"email": AdminEmail,
			"name": map[string]string{
				"first": AdminName,
			},
		},
	)
	body.SetCredentials(k.getIdentityCredentials(getDefaultAdminPassword()))

	createdIdentity, _, err := k.adminClient.IdentityApi.CreateIdentity(ctx).CreateIdentityBody(body).Execute()
	if err != nil {
		return "", err
	}

	return createdIdentity.Id, nil
}

func (k *KratosHandler) updateAdminPassword(ctx gocontext.Context, id string) error {
	body := *client.NewUpdateIdentityBody(
		"default", client.IDENTITYSTATE_ACTIVE,
		map[string]any{
			"email": AdminEmail,
			"name": map[string]string{
				"first": AdminName,
			},
		},
	)
	body.SetCredentials(k.getIdentityCredentials(getDefaultAdminPassword()))

	_, _, err := k.adminClient.IdentityApi.UpdateIdentity(ctx, id).UpdateIdentityBody(body).Execute()
	return err
}

func (k *KratosHandler) CreateAdminUser(ctx context.Context) (string, error) {
	var id string
	tx := ctx.DB().Raw(`SELECT id FROM identities WHERE traits->>'email' = ?`, AdminEmail).Scan(&id)
	if tx.Error != nil {
		return "", tx.Error
	}

	if tx.RowsAffected == 0 {
		return k.createAdminIdentity(ctx)
	}

	// Overwrite password in case database is reset
	if err := k.updateAdminPassword(ctx, id); err != nil {
		return "", fmt.Errorf("error updating admin password: %w", err)
	}

	{
		// If in case the admin identity wasn't synced with the people table, we sync it now.
		var admin models.Person
		if err := ctx.DB().Where("id = ?", id).Find(&admin).Error; err != nil {
			return "", err
		} else if admin.ID == uuid.Nil {
			// Do a dummy update so the postgres tirgger syncs the admin person.
			// This way we have the sync logic in one place.
			if err := ctx.DB().Raw(`UPDATE identities SET traits = traits WHERE id = ?`, id).Error; err != nil {
				return "", tx.Error
			}
		}
	}

	return id, nil
}
