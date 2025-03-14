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
	ID         string `json:"id"`
	Title      string `json:"title"`
	Emoji      string `json:"emoji"`
	ResourceID string `json:"resource_id"`

	// NOTE: Parameters is a list of map instead of a map because
	// we need to provide the exact schema to the LLMs.
	// i.e. the JSON schema would need to contain the exact parameters of the playbook.
	// That's not possible as we don't know which playbook the LLM will pick.
	//
	// Making it a list of map allows us to pass in a strict type-safe schema to the LLM.
	Parameters []PlaybookParameters `json:"parameters"`
}

type PlaybookParameters struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
