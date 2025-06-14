package api

type GenerateAgentRequest struct {
	Name       string
	Properties map[string]string
}

type GeneratedAgent struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}

type GenerateTokenRequest struct {
	AgentName string
}

type GeneratedToken struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}
