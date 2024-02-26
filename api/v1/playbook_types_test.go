package v1

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlaybookSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		invalid bool
	}{
		{
			name:    "invalid playbook",
			spec:    "playbook-invalid.json",
			invalid: true,
		},
		{
			name: "valid playbook",
			spec: "playbook-valid.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.spec))
			if err != nil {
				t.Errorf("PlaybookSpec.Validate() error = %v, wantErr %v", err, tt.invalid)
			}

			validationError, err := ValidatePlaybookSpec(data)
			if err != nil {
				t.Errorf("PlaybookSpec.Validate() error = %v, wantErr %v", err, tt.invalid)
			}

			if (validationError != nil) != tt.invalid {
				t.Errorf("PlaybookSpec.Validate() error = %v, wantErr %v", validationError, tt.invalid)
			}
		})
	}
}
