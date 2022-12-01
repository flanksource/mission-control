package auth

import (
	client "github.com/ory/client-go"
)

type KratosHandler struct {
	client      *client.APIClient
	adminClient *client.APIClient
}

func NewKratosHandler(kratosAPI, kratosAdminAPI string) *KratosHandler {
	return &KratosHandler{
		client:      newAPIClient(kratosAPI),
		adminClient: newAdminAPIClient(kratosAdminAPI),
	}
}

func newAPIClient(kratosAPI string) *client.APIClient {
	configuration := client.NewConfiguration()
	configuration.Servers = []client.ServerConfiguration{
		{
			URL: kratosAPI,
		},
	}
	return client.NewAPIClient(configuration)
}

func newAdminAPIClient(kratosAdminAPI string) *client.APIClient {
	configuration := client.NewConfiguration()
	configuration.Servers = []client.ServerConfiguration{
		{
			URL: kratosAdminAPI,
		},
	}
	return client.NewAPIClient(configuration)
}
