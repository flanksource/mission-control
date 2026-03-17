package connectors

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ParseAzureDevopsRepo", func() {
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

	for _, tt := range tests {
		ginkgo.It("parses "+tt.url, func() {
			org, project, repo, ok := parseAzureDevopsRepo(tt.url)
			Expect(ok).To(Equal(tt.expectedOk))
			Expect(org).To(Equal(tt.expectedOrg))
			Expect(project).To(Equal(tt.expectedProject))
			Expect(repo).To(Equal(tt.expectedRepo))
		})
	}
})

var _ = ginkgo.Describe("ParseRepoURL", func() {
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

	for _, tt := range tests {
		ginkgo.It("parses "+tt.repoURL, func() {
			hostURL, owner, repo, err := parseRepoURL(tt.repoURL)
			if tt.expectedOk {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
			Expect(owner).To(Equal(tt.expectedOwner))
			Expect(repo).To(Equal(tt.expectedRepo))
			Expect(hostURL).To(Equal(tt.host))
		})
	}
})
