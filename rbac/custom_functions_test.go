package rbac

import (
	"testing"

	"github.com/flanksource/duty/models"
)

func Test_matchPerm(t *testing.T) {
	type args struct {
		obj     any
		_agents any
		_tags   string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "string object",
			args: args{obj: "catalog", _agents: "()", _tags: "namespace=default"},
			want: true,
		},
		{
			name: "json object",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{Tags: map[string]string{
						"namespace": "default",
					}}.AsMap(),
				},
				_agents: "",
				_tags:   "namespace=default",
			},
			want: true,
		},
		{
			name: "json object tags fully not match",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{Tags: map[string]string{
						"namespace": "default",
					}}.AsMap(),
				},
				_agents: "",
				_tags:   "namespace=default,cluster=homelab",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		if tt.name != "son object tags fully not match" {
			continue
		}

		t.Run(tt.name, func(t *testing.T) {
			got, err := matchPerm(tt.args.obj, tt.args._agents, tt.args._tags)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchPerm() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("matchPerm() = %v, want %v", got, tt.want)
			}
		})
	}
}
