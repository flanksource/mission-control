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

func TestParseGitlabRepo(t *testing.T) {
	tests := []struct {
		repoURL       string
		host          string
		custom        bool
		expectedOwner string
		expectedRepo  string
		expectedOk    bool
	}{
		{"https://gitlab.com/foo/bar.git", "https://gitlab.com", false, "foo", "bar", true},
		{"https://gitlab.com/my-group/my-project", "https://gitlab.com", false, "my-group", "my-project", true},
		{"https://gitlab.flanksource.com/acme/project.git", "https://gitlab.flanksource.com", true, "acme", "project", true},
		{"https://github.com/flanksource/duty.git", "https://github.com", false, "flanksource", "duty", true},
		{"https://github.com/adityathebe/homelab", "https://github.com", false, "adityathebe", "homelab", true},
	}

	for _, tc := range tests {
		hostURL, owner, repo, err := parseRepoURL(tc.repoURL)
		if owner != tc.expectedOwner || repo != tc.expectedRepo || (err != nil) == tc.expectedOk || hostURL != tc.host {
			t.Errorf("parseGitlabRepo(%q, %t, %q) = %q, %q, %q, %v; want %q, %q, %v",
				tc.repoURL, tc.custom, tc.host, owner, repo, hostURL, err, tc.expectedOwner, tc.expectedRepo, tc.expectedOk)
		}
	}
}
