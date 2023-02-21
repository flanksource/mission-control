package api

type HTTPErrorMessage struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type HTTPSuccessMessage struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
