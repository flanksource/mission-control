package llm

type DiagnosisReport struct {
	RecommendedFix string `json:"recommended_fix"`
	Headline       string `json:"headline"`
	Summary        string `json:"summary"`
}

type PlaybookRecommendations struct {
	Playbooks []RecommendedPlaybook `json:"playbooks"`
}

type RecommendedPlaybook struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Emoji      string            `json:"emoji"`
	Parameters map[string]string `json:"parameters"`
	ResourceID string            `json:"resource_id"`
}
