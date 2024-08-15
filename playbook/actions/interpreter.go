package actions

import (
	"bufio"
	"os/exec"
	"runtime"
	"strings"

	"github.com/flanksource/duty/context"
)

var DefaultInterpreter string
var DefaultInterpreterArgs []string

func init() {
	DefaultInterpreter, DefaultInterpreterArgs = DetectDefaultInterpreter()
}

// CreateCommandFromScript creates an os/exec.Cmd from the script, using the interpreter specified in the shebang line if present.
func CreateCommandFromScript(ctx context.Context, script string) (*exec.Cmd, error) {
	interpreter, args := DetectInterpreterFromShebang(script)
	script = TrimLine(script, "#!")
	if script == "" {
		return nil, ctx.Oops().Errorf("empty script")
	}
	args = append(args, script)
	return exec.CommandContext(ctx, interpreter, args...), nil
}

func TrimLine(lines string, prefix string) string {
	s := []string{}
	for _, line := range strings.Split(lines, "\n") {
		if !strings.HasPrefix(line, prefix) {
			s = append(s, line)
		}
	}
	return strings.Join(s, "\n")
}

// DetectInterpreterFromShebang reads the first line of the script to detect the interpreter from the shebang line.
func DetectInterpreterFromShebang(script string) (string, []string) {
	reader := strings.NewReader(script)
	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		firstLine := scanner.Text()
		if strings.HasPrefix(firstLine, "#!") {
			parts := strings.Fields(strings.TrimSpace(firstLine[2:]))
			if len(parts) == 0 {
				return "", nil
			} else if len(parts) == 1 {
				// No args, just interpreter and assume it supports the -c flag
				return parts[0], []string{"-c"}
			}
			return parts[0], parts[1:]
		}
	}
	return DefaultInterpreter, DefaultInterpreterArgs
}

// DetectDefaultInterpreter detects the default interpreter based on the OS.
func DetectDefaultInterpreter() (string, []string) {
	switch runtime.GOOS {
	case "windows":
		// Check for PowerShell on Windows
		if _, err := exec.LookPath("pwsh.exe"); err == nil {
			return "pwsh.exe", []string{"-c"}
		}
		// Fallback to cmd if PowerShell is not found
		if _, err := exec.LookPath("cmd.exe"); err == nil {
			return "cmd.exe", []string{"-c"}
		}

	default:
		// Check for Bash on Unix-like systems
		if _, err := exec.LookPath("bash"); err == nil {
			return "bash", []string{"-c"}
		}
		// Fallback to sh if Bash is not found
		if _, err := exec.LookPath("sh"); err == nil {
			return "sh", []string{"-c"}
		}
	}
	return "", nil
}
