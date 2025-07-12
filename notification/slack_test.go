package notification

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestTrimmedLabels(t *testing.T) {
	testdata := []struct {
		whitelist string
		labels    map[string]string
		expected  map[string]string
	}{
		{
			whitelist: "app|batch.kubernetes.io/jobname|app.kubernetes.io/name;app.kubernetes.io/version|",
			labels: map[string]string{
				"app":                         "my-app",
				"batch.kubernetes.io/jobname": "my-job",
				"app.kubernetes.io/name":      "my-name",
				"app.kubernetes.io/version":   "1.0.0",
			},
			expected: map[string]string{
				"app":                       "my-app",
				"app.kubernetes.io/version": "1.0.0",
			},
		},
		{
			whitelist: "",
			labels: map[string]string{
				"app":     "my-app",
				"version": "1.0.0",
			},
			expected: map[string]string{},
		},
		{
			whitelist: "nonexistent|missing;another|notfound",
			labels: map[string]string{
				"app":     "my-app",
				"version": "1.0.0",
			},
			expected: map[string]string{},
		},
		{
			whitelist: "app;version;environment",
			labels: map[string]string{
				"app":         "my-app",
				"version":     "1.0.0",
				"environment": "prod",
				"team":        "backend",
			},
			expected: map[string]string{
				"app":         "my-app",
				"version":     "1.0.0",
				"environment": "prod",
			},
		},
	}

	for _, test := range testdata {
		t.Run(test.whitelist, func(t *testing.T) {
			g := gomega.NewWithT(t)

			trimmed := TrimLabels(test.whitelist, test.labels)
			g.Expect(trimmed).To(gomega.Equal(test.expected))
		})
	}
}
