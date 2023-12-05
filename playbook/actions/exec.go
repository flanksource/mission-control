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
	"time"

	"github.com/flanksource/artifacts"
	fileUtils "github.com/flanksource/commons/files"
	"github.com/flanksource/commons/logger"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/hashicorp/go-getter"
)

var checkoutLocks = utils.NamedLock{}

type ExecAction struct {
}

type ExecDetails struct {
	Error error `json:"-"`

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

	execEnvParam, err := c.prepareEnvironment(ctx, exec)
	if err != nil {
		return nil, err
	}

	switch runtime.GOOS {
	case "windows":
		return execPowershell(ctx, exec, execEnvParam)
	default:
		return execBash(ctx, exec, execEnvParam)
	}
}

func execPowershell(ctx context.Context, check v1.ExecAction, envParams *execEnv) (*ExecDetails, error) {
	ps, err := osExec.LookPath("powershell.exe")
	if err != nil {
		return nil, err
	}
	args := []string{check.Script}
	cmd := osExec.CommandContext(ctx, ps, args...)
	if len(envParams.envs) != 0 {
		cmd.Env = append(os.Environ(), envParams.envs...)
	}
	if envParams.mountPoint != "" {
		cmd.Dir = envParams.mountPoint
	}

	return runCmd(ctx, cmd, check.Artifacts...)
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

func execBash(ctx context.Context, check v1.ExecAction, execEnvParam *execEnv) (*ExecDetails, error) {
	if len(check.Script) == 0 {
		return nil, fmt.Errorf("no script provided")
	}

	cmd := osExec.CommandContext(ctx, "bash", "-c", check.Script)
	if len(execEnvParam.envs) != 0 {
		cmd.Env = append(os.Environ(), execEnvParam.envs...)
	}
	if execEnvParam.mountPoint != "" {
		cmd.Dir = execEnvParam.mountPoint
	}

	if err := setupConnection(ctx, check, cmd); err != nil {
		return nil, fmt.Errorf("failed to setup connection: %w", err)
	}

	return runCmd(ctx, cmd, check.Artifacts...)
}

func runCmd(ctx context.Context, cmd *osExec.Cmd, artifactConfigs ...v1.Artifact) (*ExecDetails, error) {
	var (
		result ExecDetails
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if ctx.IsTrace() {
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
		cmd.Stdout = io.MultiWriter(&stdout, os.Stdout)
	}

	result.Error = cmd.Run()
	result.ExitCode = cmd.ProcessState.ExitCode()
	result.Stderr = strings.TrimSpace(stderr.String())
	result.Stdout = strings.TrimSpace(stdout.String())

	for _, artifactConfig := range artifactConfigs {
		switch artifactConfig.Path {
		case "/dev/stdout":
			result.Artifacts = append(result.Artifacts, artifacts.Artifact{
				Content: io.NopCloser(strings.NewReader(result.Stdout)),
				Path:    "stdout",
			})

		case "/dev/stderr":
			result.Artifacts = append(result.Artifacts, artifacts.Artifact{
				Content: io.NopCloser(strings.NewReader(result.Stderr)),
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
				result.Artifacts = append(result.Artifacts, artifact)
			}
		}
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("non-zero exit-code: %d. (stdout=%s) (stderr=%s)", result.ExitCode, result.Stdout, result.Stderr)
	}

	return &result, nil
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

type execEnv struct {
	envs       []string
	mountPoint string
}

func (c *ExecAction) prepareEnvironment(ctx context.Context, check v1.ExecAction) (*execEnv, error) {
	var result execEnv

	for _, env := range check.EnvVars {
		val, err := ctx.GetEnvValueFromCache(env)
		if err != nil {
			return nil, fmt.Errorf("error fetching env value (name=%s): %w", env.Name, err)
		}

		result.envs = append(result.envs, fmt.Sprintf("%s=%s", env.Name, val))
	}

	if check.Checkout != nil {
		var err error
		var connection *models.Connection
		if connection, err = ctx.HydrateConnectionByURL(check.Checkout.Connection); err != nil {
			return nil, fmt.Errorf("error hydrating connection: %w", err)
		} else if connection == nil {
			connection = &models.Connection{Type: models.ConnectionTypeGit}
		}

		if connection, err = connection.Merge(ctx, check.Checkout); err != nil {
			return nil, err
		}
		var goGetterURL string
		if goGetterURL, err = connection.AsGoGetterURL(); err != nil {
			return nil, err
		}
		if goGetterURL == "" {
			return nil, fmt.Errorf("missing URL %v", *connection)
		}

		result.mountPoint = utils.Deref(check.Checkout.Destination)
		if result.mountPoint == "" {
			result.mountPoint = filepath.Join(os.TempDir(), "exec-checkout", hash.Sha256Hex(goGetterURL))
		}
		// We allow multiple checks to use the same checkout location, for disk space and performance reasons
		// however git does not allow multiple operations to be performed, so we need to lock it
		lock := checkoutLocks.TryLock(result.mountPoint, 5*time.Minute)
		if lock == nil {
			return nil, fmt.Errorf("failed to acquire checkout lock for %s", result.mountPoint)
		}
		defer lock.Release()

		if err := checkout(ctx, goGetterURL, result.mountPoint); err != nil {
			return nil, fmt.Errorf("error checking out: %w", err)
		}
	}

	return &result, nil
}

// Getter gets a directory or file using the Hashicorp go-getter library
// See https://github.com/hashicorp/go-getter
func checkout(ctx context.Context, url, dst string) error {
	pwd, _ := os.Getwd()

	stashed := false
	if fileUtils.Exists(dst + "/.git") {
		if r, err := run(ctx, dst, "git", "status", "-s"); err != nil {
			return err
		} else if r.Stdout != "" {
			if r2, err := run(ctx, dst, "git", "stash"); err != nil {
				return err
			} else if r2.Error != nil {
				return r2.Error
			}

			stashed = true
		}
	}

	client := &getter.Client{
		Ctx:     ctx,
		Src:     url,
		Dst:     dst,
		Pwd:     pwd,
		Mode:    getter.ClientModeDir,
		Options: []getter.ClientOption{},
	}
	if err := client.Get(); err != nil {
		return err
	}
	if stashed {
		if r, err := run(ctx, dst, "git", "stash", "pop"); err != nil {
			return fmt.Errorf("failed to pop: %v", err)
		} else if r.Error != nil {
			return fmt.Errorf("failed to pop: %v", r.Error)
		}
	}

	return nil
}

func run(ctx context.Context, cwd string, name string, args ...string) (*ExecDetails, error) {
	cmd := osExec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	return runCmd(ctx, cmd)
}
