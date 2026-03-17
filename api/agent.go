package api

type GenerateAgentRequest struct {
	Name       string            `json:"name"`
	AutoRenew  bool              `json:"auto_renew"`
	Properties map[string]string `json:"properties"`
}

type GeneratedAgent struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}

type GenerateTokenRequest struct {
	AgentName string
	AutoRenew bool `json:"auto_renew"`
}

type GeneratedToken struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}
