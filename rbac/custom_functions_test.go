package rbac

import (
	"testing"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
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
			want: false,
		},
		{
			name: "tag only match",
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
			name: "multiple tags match",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{
						Tags: map[string]string{
							"namespace": "default",
							"cluster":   "homelab",
						},
					}.AsMap(),
				},
				_agents: "",
				_tags:   "namespace=default,cluster=homelab",
			},
			want: true,
		},
		{
			name: "multiple tags no match",
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
		{
			name: "tags & agents match",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{
						Tags: map[string]string{
							"namespace": "default",
						},
						AgentID: uuid.MustParse("66eda456-315f-455a-95d4-6ef059fc13a8"),
					}.AsMap(),
				},
				_agents: "66eda456-315f-455a-95d4-6ef059fc13a8",
				_tags:   "namespace=default",
			},
			want: true,
		},
		{
			name: "tags match & agent no match",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{
						Tags: map[string]string{
							"namespace": "default",
						},
						AgentID: uuid.MustParse("66eda456-315f-455a-95d4-6ef059fc13a8"),
					}.AsMap(),
				},
				_agents: "",
				_tags:   "namespace=default,cluster=homelab",
			},
			want: false,
		},
		{
			name: "tags no match & agent match",
			args: args{
				obj: map[string]any{
					"config": models.ConfigItem{
						Tags: map[string]string{
							"namespace": "default",
						},
						AgentID: uuid.MustParse("66eda456-315f-455a-95d4-6ef059fc13a8"),
					}.AsMap(),
				},
				_agents: "66eda456-315f-455a-95d4-6ef059fc13a8",
				_tags:   "namespace=mc",
			},
			want: false,
		},
	}

	for _, tt := range tests {
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
