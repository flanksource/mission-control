//go:build windows

package ui

import (
	"os/exec"
)

// expectProcessGroupSet is a no-op on Windows, where setProcessGroup does not
// configure a process group.
func expectProcessGroupSet(cmd *exec.Cmd) {}
