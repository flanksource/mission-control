//go:build !windows

package ui

import (
	"os/exec"

	. "github.com/onsi/gomega"
)

// expectProcessGroupSet asserts the dev command runs in its own process group
// on Unix, which is required for group-wide teardown signalling.
func expectProcessGroupSet(cmd *exec.Cmd) {
	Expect(cmd.SysProcAttr).NotTo(BeNil())
	Expect(cmd.SysProcAttr.Setpgid).To(BeTrue())
}
