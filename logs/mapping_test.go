package logs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_mapify(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "nil input",
			input: nil,
			want:  nil,
		},
		{
			name:  "string input",
			input: "value",
			want: map[string]string{
				"": "value",
			},
		},
		{
			name: "simple map input",
			input: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
			want: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "nested map input",
			input: map[string]any{
				"level1": map[string]any{
					"level2": "value",
				},
			},
			want: map[string]string{
				"level1.level2": "value",
			},
		},
		{
			name: "multiple nested levels",
			input: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "value",
					},
				},
			},
			want: map[string]string{
				"a.b.c": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := flatMap("", tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
