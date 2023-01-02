package api

type LogsResponse struct {
	Total   int `json:"total"`
	Results []struct {
		Timestamp string            `json:"timestamp"`
		Message   string            `json:"message"`
		Labels    map[string]string `json:"labels"`
	} `json:"results"`
}
