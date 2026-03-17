package runner

import (
	"github.com/flanksource/duty/secret"
	"github.com/flanksource/incident-commander/playbook/actions"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ScrubActionResult", func() {
	templateEnv := actions.TemplateEnv{
		Params: map[string]any{
			"password": secret.Sensitive("super_secret_password"),
			"apiKey":   secret.Sensitive("sk-12345"),
			"normal":   "not_secret",
		},
	}

	ginkgo.It("scrubs ExecDetails stdout and stderr", func() {
		result := &actions.ExecDetails{
			Stdout:   "Connected with password: super_secret_password",
			Stderr:   "Error: invalid key sk-12345",
			ExitCode: 0,
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		Expect(scrubbedExec.Stdout).ToNot(ContainSubstring("super_secret_password"))
		Expect(scrubbedExec.Stdout).To(Equal("Connected with password: [REDACTED]"))

		Expect(scrubbedExec.Stderr).ToNot(ContainSubstring("sk-12345"))
		Expect(scrubbedExec.Stderr).To(Equal("Error: invalid key [REDACTED]"))
	})

	ginkgo.It("handles nil result", func() {
		Expect(scrubActionResult(&templateEnv, nil)).To(BeNil())
	})

	ginkgo.It("handles nil scrubber", func() {
		result := &actions.ExecDetails{Stdout: "test"}
		Expect(scrubActionResult(nil, result)).To(Equal(result))
	})

	ginkgo.It("handles nil ExecDetails", func() {
		var result *actions.ExecDetails
		Expect(scrubActionResult(&templateEnv, result)).To(BeNil())
	})

	ginkgo.It("preserves non-secret content", func() {
		result := &actions.ExecDetails{
			Stdout: "Normal output with not_secret value",
			Stderr: "",
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		Expect(scrubbedExec.Stdout).To(Equal("Normal output with not_secret value"))
	})

	ginkgo.It("scrubs secrets appearing multiple times", func() {
		result := &actions.ExecDetails{
			Stdout: "sk-12345 and again sk-12345",
		}

		scrubbed := scrubActionResult(&templateEnv, result)
		scrubbedExec := scrubbed.(*actions.ExecDetails)

		Expect(scrubbedExec.Stdout).To(Equal("[REDACTED] and again [REDACTED]"))
	})
})
