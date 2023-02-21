package auth

import (
	client "github.com/ory/client-go"
)

type KratosHandler struct {
	client      *client.APIClient
	adminClient *client.APIClient
	jwtSecret   string
}

func NewKratosHandler(kratosAPI, kratosAdminAPI, jwtSecret string) *KratosHandler {
	return &KratosHandler{
		client:      newAPIClient(kratosAPI),
		adminClient: newAdminAPIClient(kratosAdminAPI),
		jwtSecret:   jwtSecret,
	}
}

func newAPIClient(kratosAPI string) *client.APIClient {
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
