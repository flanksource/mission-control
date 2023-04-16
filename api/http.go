package api

type HTTPError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type HTTPSuccess struct {
	Message string `json:"message"`
	Payload any    `json:"payload,omitempty"`
}
