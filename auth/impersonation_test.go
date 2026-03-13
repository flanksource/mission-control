// ABOUTME: Tests for scope impersonation logic that allows admins to simulate
// ABOUTME: RLS restrictions via the X-Flanksource-Scope header.
package auth

import (
	"reflect"
	"sort"
	"testing"

	"github.com/flanksource/duty/rls"
)

func TestIntersectScope(t *testing.T) {
	tests := []struct {
		name string
		a, b rls.Scope
		want rls.Scope
		ok   bool
	}{
		{
			name: "both empty",
			a:    rls.Scope{},
			b:    rls.Scope{},
			want: rls.Scope{},
			ok:   true,
		},
		{
			name: "matching tags merge",
			a:    rls.Scope{Tags: map[string]string{"team": "backend"}},
			b:    rls.Scope{Tags: map[string]string{"env": "prod"}},
			want: rls.Scope{Tags: map[string]string{"team": "backend", "env": "prod"}},
			ok:   true,
		},
		{
			name: "conflicting tags",
			a:    rls.Scope{Tags: map[string]string{"team": "backend"}},
			b:    rls.Scope{Tags: map[string]string{"team": "frontend"}},
			want: rls.Scope{},
			ok:   false,
		},
		{
			name: "overlapping agents",
			a:    rls.Scope{Agents: []string{"a", "b"}},
			b:    rls.Scope{Agents: []string{"b", "c"}},
			want: rls.Scope{Agents: []string{"b"}},
			ok:   true,
		},
		{
			name: "disjoint agents",
			a:    rls.Scope{Agents: []string{"a"}},
			b:    rls.Scope{Agents: []string{"b"}},
			want: rls.Scope{},
			ok:   false,
		},
		{
			name: "one side unrestricted agents",
			a:    rls.Scope{Agents: []string{"a", "b"}},
			b:    rls.Scope{},
			want: rls.Scope{Agents: []string{"a", "b"}},
			ok:   true,
		},
		{
			name: "overlapping names",
			a:    rls.Scope{Names: []string{"x", "y"}},
			b:    rls.Scope{Names: []string{"y", "z"}},
			want: rls.Scope{Names: []string{"y"}},
			ok:   true,
		},
		{
			name: "disjoint names",
			a:    rls.Scope{Names: []string{"x"}},
			b:    rls.Scope{Names: []string{"y"}},
			want: rls.Scope{},
			ok:   false,
		},
		{
			name: "same ID",
			a:    rls.Scope{ID: "abc"},
			b:    rls.Scope{ID: "abc"},
			want: rls.Scope{ID: "abc"},
			ok:   true,
		},
		{
			name: "different IDs",
			a:    rls.Scope{ID: "abc"},
			b:    rls.Scope{ID: "def"},
			want: rls.Scope{},
			ok:   false,
		},
		{
			name: "one ID set",
			a:    rls.Scope{ID: "abc"},
			b:    rls.Scope{},
			want: rls.Scope{ID: "abc"},
			ok:   true,
		},
		{
			name: "combined fields",
			a:    rls.Scope{Tags: map[string]string{"team": "backend"}, Agents: []string{"a", "b"}},
			b:    rls.Scope{Tags: map[string]string{"env": "prod"}, Agents: []string{"b", "c"}},
			want: rls.Scope{Tags: map[string]string{"team": "backend", "env": "prod"}, Agents: []string{"b"}},
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := intersectScope(tt.a, tt.b)
			if ok != tt.ok {
				t.Fatalf("ok: got %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			sortScope(&got)
			sortScope(&tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scope: got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIntersectScopeList(t *testing.T) {
	tests := []struct {
		name                string
		real, impersonated  []rls.Scope
		want                []rls.Scope
	}{
		{
			name:         "empty impersonated returns empty",
			real:         []rls.Scope{{ID: "a"}},
			impersonated: nil,
			want:         nil,
		},
		{
			name:         "empty real returns empty",
			real:         nil,
			impersonated: []rls.Scope{{ID: "a"}},
			want:         nil,
		},
		{
			name:         "matching IDs kept",
			real:         []rls.Scope{{ID: "a"}, {ID: "b"}},
			impersonated: []rls.Scope{{ID: "a"}},
			want:         []rls.Scope{{ID: "a"}},
		},
		{
			name:         "no overlap produces empty",
			real:         []rls.Scope{{ID: "a"}},
			impersonated: []rls.Scope{{ID: "b"}},
			want:         nil,
		},
		{
			name:         "cartesian product of tag scopes",
			real:         []rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			impersonated: []rls.Scope{{Tags: map[string]string{"env": "prod"}}},
			want:         []rls.Scope{{Tags: map[string]string{"team": "backend", "env": "prod"}}},
		},
		{
			name: "multiple real scopes with one impersonated",
			real: []rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
				{Tags: map[string]string{"team": "frontend"}},
			},
			impersonated: []rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
			},
			want: []rls.Scope{
				{Tags: map[string]string{"team": "backend"}},
				// frontend AND backend = conflict, excluded
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersectScopeList(tt.real, tt.impersonated)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyImpersonation(t *testing.T) {
	tests := []struct {
		name        string
		real        *rls.Payload
		impersonate *rls.Payload
		want        *rls.Payload
		wantErr     bool
	}{
		{
			name:        "no impersonation returns real payload",
			real:        &rls.Payload{Disable: true},
			impersonate: nil,
			want:        &rls.Payload{Disable: true},
		},
		{
			name: "admin can impersonate scopes",
			real: &rls.Payload{Disable: true},
			impersonate: &rls.Payload{
				Config: []rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			},
			want: &rls.Payload{
				Config: []rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			},
		},
		{
			name: "guest impersonation intersects with real payload",
			real: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-1"}, {ID: "cfg-2"}},
			},
			impersonate: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-1"}, {ID: "cfg-3"}},
			},
			want: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-1"}},
			},
		},
		{
			name: "guest impersonation with no overlap restricts to nothing",
			real: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-1"}},
			},
			impersonate: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-99"}},
			},
			want: &rls.Payload{},
		},
		{
			name: "guest impersonation intersects scopes list",
			real: &rls.Payload{
				Scopes: []string{"scope-a", "scope-b", "scope-c"},
			},
			impersonate: &rls.Payload{
				Scopes: []string{"scope-b", "scope-d"},
			},
			want: &rls.Payload{
				Scopes: []string{"scope-b"},
			},
		},
		{
			name:        "impersonation with empty payload restricts to nothing",
			real:        &rls.Payload{Disable: true},
			impersonate: &rls.Payload{},
			want:        &rls.Payload{},
		},
		{
			name: "guest with multiple resource types",
			real: &rls.Payload{
				Config:    []rls.Scope{{ID: "cfg-1"}},
				Component: []rls.Scope{{Tags: map[string]string{"team": "backend"}}},
			},
			impersonate: &rls.Payload{
				Config:    []rls.Scope{{ID: "cfg-1"}},
				Component: []rls.Scope{{Tags: map[string]string{"team": "frontend"}}},
			},
			want: &rls.Payload{
				Config: []rls.Scope{{ID: "cfg-1"}},
				// Component: backend AND frontend = conflict, excluded
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyImpersonation(tt.real, tt.impersonate)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Disable != tt.want.Disable {
				t.Errorf("Disable: got %v, want %v", got.Disable, tt.want.Disable)
			}
			if !scopeListsEqual(got.Config, tt.want.Config) {
				t.Errorf("Config: got %+v, want %+v", got.Config, tt.want.Config)
			}
			if !scopeListsEqual(got.Component, tt.want.Component) {
				t.Errorf("Component: got %+v, want %+v", got.Component, tt.want.Component)
			}
			if !scopeListsEqual(got.Playbook, tt.want.Playbook) {
				t.Errorf("Playbook: got %+v, want %+v", got.Playbook, tt.want.Playbook)
			}
			if !scopeListsEqual(got.Canary, tt.want.Canary) {
				t.Errorf("Canary: got %+v, want %+v", got.Canary, tt.want.Canary)
			}
			if !scopeListsEqual(got.View, tt.want.View) {
				t.Errorf("View: got %+v, want %+v", got.View, tt.want.View)
			}
			if !stringSlicesEqual(got.Scopes, tt.want.Scopes) {
				t.Errorf("Scopes: got %+v, want %+v", got.Scopes, tt.want.Scopes)
			}
		})
	}
}

// sortScope sorts slice fields for deterministic comparison.
func sortScope(s *rls.Scope) {
	sort.Strings(s.Agents)
	sort.Strings(s.Names)
}

func scopeListsEqual(a, b []rls.Scope) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
