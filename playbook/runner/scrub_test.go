package runner

import (
	"testing"

	"github.com/flanksource/duty/secret"
	"github.com/flanksource/incident-commander/playbook/actions"
	. "github.com/onsi/gomega"
)

func TestScrubActionResult(t *testing.T) {
	templateEnv := actions.TemplateEnv{
		Params: map[string]any{
			"password": secret.Sensitive("super_secret_password"),
			"apiKey":   secret.Sensitive("sk-12345"),
			"normal":   "not_secret",
		},
	}

	t.Run("scrubs ExecDetails stdout and stderr", func(t *testing.T) {
		g := NewWithT(t)

		result := &actions.ExecDetails{
			Stdout:   "Connected with password: super_secret_password",
			Stderr:   "Error: invalid key sk-12345",
			ExitCode: 0,
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		g.Expect(scrubbedExec.Stdout).ToNot(ContainSubstring("super_secret_password"))
		g.Expect(scrubbedExec.Stdout).To(Equal("Connected with password: [REDACTED]"))

		g.Expect(scrubbedExec.Stderr).ToNot(ContainSubstring("sk-12345"))
		g.Expect(scrubbedExec.Stderr).To(Equal("Error: invalid key [REDACTED]"))
	})

	t.Run("handles nil result", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(scrubActionResult(&templateEnv, nil)).To(BeNil())
	})

	t.Run("handles nil scrubber", func(t *testing.T) {
		g := NewWithT(t)
		result := &actions.ExecDetails{Stdout: "test"}
		g.Expect(scrubActionResult(nil, result)).To(Equal(result))
	})

	t.Run("handles nil ExecDetails", func(t *testing.T) {
		g := NewWithT(t)
		var result *actions.ExecDetails
		g.Expect(scrubActionResult(&templateEnv, result)).To(BeNil())
	})

	t.Run("preserves non-secret content", func(t *testing.T) {
		g := NewWithT(t)

		result := &actions.ExecDetails{
			Stdout: "Normal output with not_secret value",
			Stderr: "",
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		g.Expect(scrubbedExec.Stdout).To(Equal("Normal output with not_secret value"))
	})

	t.Run("scrubs secrets appearing multiple times", func(t *testing.T) {
		g := NewWithT(t)

		result := &actions.ExecDetails{
			Stdout: "sk-12345 and again sk-12345",
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		g.Expect(scrubbedExec.Stdout).To(Equal("[REDACTED] and again [REDACTED]"))
	})
}
