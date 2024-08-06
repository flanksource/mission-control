package actions

import (
	"bufio"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/samber/lo"
)

var DefaultInterpreter string

func init() {
	DefaultInterpreter = DetectDefaultInterpreter()
}

// ParseShebangLine reads the first line of the script to detect the interpreter from the shebang line.
func ParseShebangLine(script string) (string, []string) {
	reader := strings.NewReader(script)
	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		firstLine := scanner.Text()
		if strings.HasPrefix(firstLine, "#!") {
			parts := strings.Fields(strings.TrimSpace(firstLine[2:]))
			if len(parts) > 0 {
				return parts[0], parts[1:]
			}
		}
	}
	return "", nil
}

// CreateCommandFromScript creates an os/exec.Cmd from the script, using the interpreter specified in the shebang line if present.
func CreateCommandFromScript(ctx context.Context, script string) (*exec.Cmd, error) {
	interpreter, args := ParseShebangLine(script)
	args = append([]string{"-c", script}, args...)
	return exec.CommandContext(ctx, lo.CoalesceOrEmpty(interpreter, DefaultInterpreter), args...), nil
}

// DetectInterpreterFromShebang reads the first line of the script to detect the interpreter from the shebang line.
func DetectInterpreterFromShebang(script string) (string, error) {
	reader := strings.NewReader(script)
	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		firstLine := scanner.Text()
		if strings.HasPrefix(firstLine, "#!") {
			return strings.TrimSpace(firstLine[2:]), nil
		}
	}
	return "", fmt.Errorf("no shebang line found")
}

// DetectDefaultInterpreter detects the default interpreter based on the OS.
func DetectDefaultInterpreter() string {
	switch runtime.GOOS {
	case "windows":
		// Check for PowerShell on Windows
		if _, err := exec.LookPath("pwsh.exe"); err == nil {
			return "pwsh.exe"
		}
		// Fallback to cmd if PowerShell is not found
		if _, err := exec.LookPath("cmd.exe"); err == nil {
			return "cmd.exe"
		}
		return ""

	default:
		// Check for Bash on Unix-like systems
		if _, err := exec.LookPath("bash"); err == nil {
			return "bash"
		}
		// Fallback to sh if Bash is not found
		if _, err := exec.LookPath("sh"); err == nil {
			return "sh"
		}
		return ""
	}
}
