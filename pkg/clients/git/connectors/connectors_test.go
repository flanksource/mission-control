package connectors

import (
	"testing"
)

func TestParseAzureDevopsRepo(t *testing.T) {
	tests := []struct {
		url             string
		expectedOrg     string
		expectedProject string
		expectedRepo    string
		expectedOk      bool
	}{
		{"https://flanksource@dev.azure.com/flanksource/Demo1/_git/infra", "flanksource", "Demo1", "infra", true},
		{"https://orgname@dev.azure.com/orgname/projectname/_git/demo", "orgname", "projectname", "demo", true},
		{"https://invalid-url.com", "", "", "", false},
	}

	for _, test := range tests {
		org, project, repo, ok := parseAzureDevopsRepo(test.url)
		if ok != test.expectedOk {
			t.Errorf("For URL %s, expected ok: %t, but got ok: %t", test.url, test.expectedOk, ok)
		}

		if org != test.expectedOrg || project != test.expectedProject || repo != test.expectedRepo {
			t.Errorf("For URL %s, expected org: %s, project: %s, repo: %s, but got org: %s, project: %s, ok: %s",
				test.url, test.expectedOrg, test.expectedProject, test.expectedRepo, org, project, repo)
		}
	}
}
