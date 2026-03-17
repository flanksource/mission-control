package v1

type DurationParamProperties struct {
	Min     string   `json:"min,omitempty"`
	Max     string   `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
}
