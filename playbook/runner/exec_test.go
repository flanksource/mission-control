package runner

import (
	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/playbook/actions"
)

var _ = ginkgo.Describe("ResultStatus", func() {
	tests := []struct {
		name     string
		result   any
		expected models.PlaybookActionStatus
	}{
		{
			name:     "ai action that triggered child runs",
			result:   &actions.AIActionResult{ChildRunsTriggered: 1},
			expected: models.PlaybookActionStatusWaitingChildren,
		},
		{
			name:     "ai action with a diagnosis",
			result:   &actions.AIActionResult{JSON: `{"headline": "OOMKilled"}`},
			expected: models.PlaybookActionStatusCompleted,
		},
		{
			name:     "exec action with non-zero exit code",
			result:   &actions.ExecDetails{ExitCode: 1},
			expected: models.PlaybookActionStatusFailed,
		},
		{
			name:     "exec action with zero exit code",
			result:   &actions.ExecDetails{Stdout: "ok"},
			expected: models.PlaybookActionStatusCompleted,
		},
		{
			name:     "typed nil result",
			result:   (*actions.AIActionResult)(nil),
			expected: models.PlaybookActionStatusCompleted,
		},
		{
			name:     "nil result",
			result:   nil,
			expected: models.PlaybookActionStatusCompleted,
		},
		{
			name:     "result without a status",
			result:   &actions.NotificationResult{Message: "hello"},
			expected: models.PlaybookActionStatusCompleted,
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			Expect(resultStatus(tt.result)).To(Equal(tt.expected))
		})
	}

	// extractContentType marshals results into a generic map that no longer
	// implements StatusAccessor, so the status must be read before it runs.
	ginkgo.It("must be read before extractContentType transforms the result", func() {
		result := any(&actions.AIActionResult{ChildRunsTriggered: 1})
		Expect(resultStatus(result)).To(Equal(models.PlaybookActionStatusWaitingChildren))

		transformed := extractContentType(result, "ai", "")
		Expect(resultStatus(transformed)).To(Equal(models.PlaybookActionStatusCompleted))
	})
})
