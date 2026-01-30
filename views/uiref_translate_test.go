package views

import (
	"testing"

	"github.com/flanksource/incident-commander/api"
)

func TestTranslateTristate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		oldSep string
		newSep string
		want   string
	}{
		{
			name:   "simple include",
			input:  "diff,create,delete",
			oldSep: "",
			newSep: "",
			want:   "diff:1,create:1,delete:1",
		},
		{
			name:   "with exclude",
			input:  "diff,-BackOff,-CrashLoopBackOff",
			oldSep: "",
			newSep: "",
			want:   "diff:1,BackOff:-1,CrashLoopBackOff:-1",
		},
		{
			name:   "config types with separator replacement",
			input:  "AWS::Account,-Kubernetes::Pod",
			oldSep: "::",
			newSep: "__",
			want:   "AWS__Account:1,Kubernetes__Pod:-1",
		},
		{
			name:   "empty input",
			input:  "",
			oldSep: "",
			newSep: "",
			want:   "",
		},
		{
			name:   "only excludes",
			input:  "-value1,-value2",
			oldSep: "",
			newSep: "",
			want:   "value1:-1,value2:-1",
		},
		{
			name:   "mixed with spaces",
			input:  " diff , -BackOff , create ",
			oldSep: "",
			newSep: "",
			want:   "diff:1,BackOff:-1,create:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateTristate(tt.input, tt.oldSep, tt.newSep)
			if got != tt.want {
				t.Errorf("translateTristate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranslateTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple include",
			input: "env=production",
			want:  "env____production:1",
		},
		{
			name:  "include and exclude",
			input: "env=production,!env=staging",
			want:  "env____production:1,env____staging:-1",
		},
		{
			name:  "multiple includes and excludes",
			input: "env=prod,team=platform,!region=us-east-1",
			want:  "env____prod:1,team____platform:1,region____us-east-1:-1",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "only excludes",
			input: "!team=dev,!env=staging",
			want:  "team____dev:-1,env____staging:-1",
		},
		{
			name:  "with spaces",
			input: " env = prod , ! env = staging ",
			want:  "env____prod:1,env____staging:-1",
		},
		{
			name:  "invalid format skipped",
			input: "env=prod,invalid,no_equals_here,team=platform",
			want:  "env____prod:1,team____platform:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateTags(tt.input)
			if got != tt.want {
				t.Errorf("translateTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranslateChangesFilters(t *testing.T) {
	input := &api.ChangesUIFilters{
		ConfigTypes: "AWS::Account,-Kubernetes::Pod",
		ChangeType:  "diff,-BackOff",
		Severity:    "high",
		From:        "24h",
		To:          "",
		Tags:        "env=prod,!env=staging",
		Source:      "kubernetes,-github",
		Summary:     "-Failed",
		CreatedBy:   "user@example.com,-bot",
	}

	got := translateChangesFilters(input)

	// Verify tristate translations
	if got.ConfigTypes != "AWS__Account:1,Kubernetes__Pod:-1" {
		t.Errorf("ConfigTypes = %v, want %v", got.ConfigTypes, "AWS__Account:1,Kubernetes__Pod:-1")
	}
	if got.ChangeType != "diff:1,BackOff:-1" {
		t.Errorf("ChangeType = %v, want %v", got.ChangeType, "diff:1,BackOff:-1")
	}
	if got.Source != "kubernetes:1,github:-1" {
		t.Errorf("Source = %v, want %v", got.Source, "kubernetes:1,github:-1")
	}
	if got.Summary != "Failed:-1" {
		t.Errorf("Summary = %v, want %v", got.Summary, "Failed:-1")
	}
	if got.CreatedBy != "user@example.com:1,bot:-1" {
		t.Errorf("CreatedBy = %v, want %v", got.CreatedBy, "user@example.com:1,bot:-1")
	}

	// Verify tags translation
	if got.Tags != "env____prod:1,env____staging:-1" {
		t.Errorf("Tags = %v, want %v", got.Tags, "env____prod:1,env____staging:-1")
	}

	// Verify passthrough fields
	if got.Severity != "high" {
		t.Errorf("Severity = %v, want %v", got.Severity, "high")
	}
	if got.From != "24h" {
		t.Errorf("From = %v, want %v", got.From, "24h")
	}
}

func TestTranslateConfigsFilters(t *testing.T) {
	input := &api.ConfigsUIFilters{
		Search:     "database",
		ConfigType: "AWS::RDS::Instance",
		Labels:     "app=nginx,!team=dev",
		Status:     "Running,-Stopped",
		Health:     "-healthy,warning",
	}

	got := translateConfigsFilters(input)

	// Verify passthrough fields
	if got.Search != "database" {
		t.Errorf("Search = %v, want %v", got.Search, "database")
	}
	if got.ConfigType != "AWS::RDS::Instance" {
		t.Errorf("ConfigType = %v, want %v", got.ConfigType, "AWS::RDS::Instance")
	}

	// Verify tristate translations
	if got.Status != "Running:1,Stopped:-1" {
		t.Errorf("Status = %v, want %v", got.Status, "Running:1,Stopped:-1")
	}
	if got.Health != "healthy:-1,warning:1" {
		t.Errorf("Health = %v, want %v", got.Health, "healthy:-1,warning:1")
	}

	// Verify labels translation
	if got.Labels != "app____nginx:1,team____dev:-1" {
		t.Errorf("Labels = %v, want %v", got.Labels, "app____nginx:1,team____dev:-1")
	}
}

func TestTranslateViewSections(t *testing.T) {
	sections := []api.ViewSection{
		{
			Title: "View Section",
			Icon:  "icon",
			ViewRef: &api.ViewRef{
				Namespace: "default",
				Name:      "my-view",
			},
		},
		{
			Title: "Changes Section",
			Icon:  "activity",
			UIRef: &api.UIRef{
				Changes: &api.ChangesUIFilters{
					Severity: "high",
					Tags:     "env=prod",
				},
			},
		},
		{
			Title: "Configs Section",
			Icon:  "server",
			UIRef: &api.UIRef{
				Configs: &api.ConfigsUIFilters{
					Health: "-healthy",
					Labels: "app=nginx",
				},
			},
		},
	}

	got := translateViewSections(sections)

	// First section should be unchanged (no UIRef)
	if got[0].Title != "View Section" || got[0].ViewRef == nil {
		t.Errorf("First section not preserved correctly")
	}

	// Second section should have translated Changes filters
	if got[1].UIRef == nil || got[1].UIRef.Changes == nil {
		t.Fatalf("Second section Changes filters not translated")
	}
	if got[1].UIRef.Changes.Severity != "high" {
		t.Errorf("Severity not preserved: %v", got[1].UIRef.Changes.Severity)
	}
	if got[1].UIRef.Changes.Tags != "env____prod:1" {
		t.Errorf("Tags not translated: %v, want %v", got[1].UIRef.Changes.Tags, "env____prod:1")
	}

	// Third section should have translated Configs filters
	if got[2].UIRef == nil || got[2].UIRef.Configs == nil {
		t.Fatalf("Third section Configs filters not translated")
	}
	if got[2].UIRef.Configs.Health != "healthy:-1" {
		t.Errorf("Health not translated: %v, want %v", got[2].UIRef.Configs.Health, "healthy:-1")
	}
	if got[2].UIRef.Configs.Labels != "app____nginx:1" {
		t.Errorf("Labels not translated: %v, want %v", got[2].UIRef.Configs.Labels, "app____nginx:1")
	}
}

func TestTranslateViewSection_NilUIRef(t *testing.T) {
	section := api.ViewSection{
		Title: "No UIRef",
		Icon:  "icon",
		ViewRef: &api.ViewRef{
			Namespace: "default",
			Name:      "my-view",
		},
	}

	got := translateViewSection(section)

	// Should be returned as-is when UIRef is nil
	if got.Title != "No UIRef" {
		t.Errorf("Title changed: %v", got.Title)
	}
	if got.UIRef != nil {
		t.Errorf("UIRef should be nil, got: %v", got.UIRef)
	}
}
