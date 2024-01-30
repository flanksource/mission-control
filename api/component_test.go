package api

import "testing"

func TestComponentSelector_Hash(t *testing.T) {
	type fields struct {
		Name      string
		Namespace string
		Selector  string
		Labels    map[string]string
		Types     Items
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "name, namespace & selector only",
			fields: fields{
				Name:      "airsonic",
				Namespace: "music",
				Selector:  ".metadata.name=airsonic",
			},
			want: "411e6d2a831b6a924b4580a142068a9aaf31e8a1474a1678b10095a146b6d325",
		},
		{
			name: "with labels",
			fields: fields{
				Name:      "airsonic",
				Namespace: "music",
				Selector:  ".metadata.name=airsonic",
				Labels: map[string]string{
					"ingress":   "nginx",
					"env":       "production",
					"monitored": "true",
				},
			},
			want: "70bdb4e690176317f77cfd0acf154d46e816082a184c4922ed24fccf2c18f2b8",
		},
		{
			name: "with labels & types",
			fields: fields{
				Name:      "airsonic",
				Namespace: "music",
				Selector:  ".metadata.name=airsonic",
				Labels: map[string]string{
					"env": "production",
				},
				Types: []string{"Kubernetes::Pod", "Kubernetes::Deployment"},
			},
			want: "0b68985c61969296d735d4538f17db4e0782672cac08d0ffc81e00ec97d0a779",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ComponentSelector{
				Name:      tt.fields.Name,
				Namespace: tt.fields.Namespace,
				Selector:  tt.fields.Selector,
				Labels:    tt.fields.Labels,
				Types:     tt.fields.Types,
			}
			if got := c.Hash(); got != tt.want {
				t.Errorf("ComponentSelector.Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}
