package api

import (
	"reflect"
	"testing"
)

func Test_sanitizeStringSliceVar(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{name: "simple", args: []string{"name=flanksource"}, want: map[string]string{"name": "flanksource"}},
		{name: "white space", args: []string{"    name  =  flanksource   "}, want: map[string]string{"name": "flanksource"}},
		{name: "multiple-simple", args: []string{"name=flanksource", "foo=bar"}, want: map[string]string{"name": "flanksource", "foo": "bar"}},
		{name: "double-equal", args: []string{"name=foo=bar"}, want: map[string]string{"name": "foo=bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeStringSliceVar(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sanitizeStringSliceVar() = %v, want %v", got, tt.want)
			}
		})
	}
}
