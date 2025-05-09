package logs

// FieldMappingConfig defines how source log fields map to canonical LogLine fields.
// Each key represents a canonical field (e.g., "message", "timestamp"),
// and the value is a list of possible source field names.
//
// +kubebuilder:object:generate=true
type FieldMappingConfig struct {
	ID        []string `json:"id,omitempty" yaml:"id,omitempty"`
	Message   []string `json:"message,omitempty" yaml:"message,omitempty"`
	Timestamp []string `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
	Host      []string `json:"host,omitempty" yaml:"host,omitempty"`
	Severity  []string `json:"severity,omitempty" yaml:"severity,omitempty"`
	Source    []string `json:"source,omitempty" yaml:"source,omitempty"`
	Ignore    []string `json:"ignore,omitempty" yaml:"ignore,omitempty"`
}

func (c FieldMappingConfig) WithDefaults(defaultMap FieldMappingConfig) FieldMappingConfig {
	if len(c.ID) == 0 {
		c.ID = defaultMap.ID
	}
	if len(c.Message) == 0 {
		c.Message = defaultMap.Message
	}
	if len(c.Timestamp) == 0 {
		c.Timestamp = defaultMap.Timestamp
	}
	if len(c.Severity) == 0 {
		c.Severity = defaultMap.Severity
	}
	if len(c.Source) == 0 {
		c.Source = defaultMap.Source
	}
	if len(c.Ignore) == 0 {
		c.Ignore = defaultMap.Ignore
	}
	if len(c.Host) == 0 {
		c.Host = defaultMap.Host
	}
	return c
}

func (c FieldMappingConfig) Empty() bool {
	return len(c.ID) == 0 && len(c.Message) == 0 && len(c.Timestamp) == 0 && len(c.Severity) == 0 && len(c.Source) == 0 && len(c.Ignore) == 0 && len(c.Host) == 0
}
