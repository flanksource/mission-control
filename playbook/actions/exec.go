package actions

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	osExec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	textTemplate "text/template"

	"github.com/flanksource/artifacts"
	fileUtils "github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

type ExecAction struct {
}

type ExecDetails struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`

	Artifacts []artifacts.Artifact `json:"-" yaml:"-"`
}

func (c *ExecAction) Run(ctx context.Context, exec v1.ExecAction, env TemplateEnv) (*ExecDetails, error) {
	script, err := gomplate.RunTemplate(env.AsMap(), gomplate.Template{Template: exec.Script})
	if err != nil {
		return nil, err
	}
	exec.Script = script

	switch runtime.GOOS {
	case "windows":
		return execPowershell(ctx, exec)
	default:
		return execBash(ctx, exec)
	}
}

func execPowershell(ctx context.Context, check v1.ExecAction) (*ExecDetails, error) {
	ps, err := osExec.LookPath("powershell.exe")
	if err != nil {
		return nil, err
	}
	args := []string{check.Script}
	cmd := osExec.CommandContext(ctx, ps, args...)
	return runCmd(cmd, check.Artifacts)
}

func setupConnection(ctx context.Context, check v1.ExecAction, cmd *osExec.Cmd) error {
	if check.Connections.AWS != nil {
		if err := check.Connections.AWS.Populate(ctx, ctx.Kubernetes(), ctx.GetNamespace()); err != nil {
			return fmt.Errorf("failed to hydrate aws connection: %w", err)
		}

		configPath, err := saveConfig(awsConfigTemplate, check.Connections.AWS)
		defer os.RemoveAll(filepath.Dir(configPath))
		if err != nil {
			return fmt.Errorf("failed to store AWS credentials: %w", err)
		}

		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "AWS_EC2_METADATA_DISABLED=true") // https://github.com/aws/aws-cli/issues/5262#issuecomment-705832151
		cmd.Env = append(cmd.Env, fmt.Sprintf("AWS_SHARED_CREDENTIALS_FILE=%s", configPath))
		if check.Connections.AWS.Region != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("AWS_DEFAULT_REGION=%s", check.Connections.AWS.Region))
		}
	}

	if check.Connections.Azure != nil {
		if err := check.Connections.Azure.HydrateConnection(ctx); err != nil {
			return fmt.Errorf("failed to hydrate connection %w", err)
		}

		// login with service principal
		runCmd := osExec.Command("az", "login", "--service-principal", "--username", check.Connections.Azure.ClientID.ValueStatic, "--password", check.Connections.Azure.ClientSecret.ValueStatic, "--tenant", check.Connections.Azure.TenantID)
		if err := runCmd.Run(); err != nil {
			return fmt.Errorf("failed to login: %w", err)
		}
	}

	if check.Connections.GCP != nil {
		if err := check.Connections.GCP.HydrateConnection(ctx); err != nil {
			return fmt.Errorf("failed to hydrate connection %w", err)
		}

		configPath, err := saveConfig(gcloudConfigTemplate, check.Connections.GCP)
		defer os.RemoveAll(filepath.Dir(configPath))
		if err != nil {
			return fmt.Errorf("failed to store gcloud credentials: %w", err)
		}

		// to configure gcloud CLI to use the service account specified in GOOGLE_APPLICATION_CREDENTIALS,
		// we need to explicitly activate it
		runCmd := osExec.Command("gcloud", "auth", "activate-service-account", "--key-file", configPath)
		if err := runCmd.Run(); err != nil {
			return fmt.Errorf("failed to activate GCP service account: %w", err)
		}

		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", configPath))
	}

	return nil
}

func execBash(ctx context.Context, check v1.ExecAction) (*ExecDetails, error) {
	if len(check.Script) == 0 {
		return nil, fmt.Errorf("no script provided")
	}

	cmd := osExec.CommandContext(ctx, "bash", "-c", check.Script)
	if err := setupConnection(ctx, check, cmd); err != nil {
		return nil, fmt.Errorf("failed to setup connection: %w", err)
	}

	return runCmd(cmd, check.Artifacts)
}

func runCmd(cmd *osExec.Cmd, artifactConfigs []v1.Artifact) (*ExecDetails, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	}

	details := ExecDetails{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		ExitCode: cmd.ProcessState.ExitCode(),
	}

	for _, artifactConfig := range artifactConfigs {
		switch artifactConfig.Path {
		case "/dev/stdout":
			details.Artifacts = append(details.Artifacts, artifacts.Artifact{
				Content: io.NopCloser(strings.NewReader(details.Stdout)),
				Path:    "stdout",
			})

		case "/dev/stderr":
			details.Artifacts = append(details.Artifacts, artifacts.Artifact{
				Content: io.NopCloser(strings.NewReader(details.Stderr)),
				Path:    "stderr",
			})

		default:
			paths, err := fileUtils.UnfoldGlobs(artifactConfig.Path)
			if err != nil {
				return nil, err
			}

			for _, path := range paths {
				artifact := artifacts.Artifact{}

				file, err := os.Open(path)
				if err != nil {
					logger.Errorf("error opening file. path=%s; %w", path, err)
					continue
				}

				artifact.Content = file
				artifact.Path = path
				details.Artifacts = append(details.Artifacts, artifact)
			}
		}
	}
	if details.ExitCode != 0 {
		return nil, fmt.Errorf("non-zero exit-code: %d. (stdout=%s) (stderr=%s)", details.ExitCode, details.Stdout, details.Stderr)
	}

	return &details, nil
}

func saveConfig(configTemplate *textTemplate.Template, view any) (string, error) {
	dirPath := filepath.Join(".creds", fmt.Sprintf("cred-%d", rand.Intn(10000000)))
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return "", err
	}

	configPath := fmt.Sprintf("%s/credentials", dirPath)
	logger.Tracef("Creating credentials file: %s", configPath)

	file, err := os.Create(configPath)
	if err != nil {
		return configPath, err
	}
	defer file.Close()

	if err := configTemplate.Execute(file, view); err != nil {
		return configPath, err
	}

	return configPath, nil
}

var (
	awsConfigTemplate    *textTemplate.Template
	gcloudConfigTemplate *textTemplate.Template
)

func init() {
	awsConfigTemplate = textTemplate.Must(textTemplate.New("").Parse(`[default]
aws_access_key_id = {{.AccessKey.ValueStatic}}
aws_secret_access_key = {{.SecretKey.ValueStatic}}
{{if .SessionToken.ValueStatic}}aws_session_token={{.SessionToken.ValueStatic}}{{end}}
`))

	gcloudConfigTemplate = textTemplate.Must(textTemplate.New("").Parse(`{{.Credentials}}`))
}
