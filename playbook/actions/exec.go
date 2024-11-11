package actions

import (
	"bytes"
	"fmt"
	"io"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/artifacts"
	fileUtils "github.com/flanksource/commons/files"

	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/hashicorp/go-getter"
)

var checkoutLocks = utils.NamedLock{}

type ExecAction struct {
}

type ExecDetails struct {
	Error error `json:"-"`

	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	ExitCode int      `json:"exitCode"`
	Path     string   `json:"path"`
	Args     []string `json:"args"`

	Artifacts []artifacts.Artifact `json:"-" yaml:"-"`
}

func (e *ExecDetails) GetArtifacts() []artifacts.Artifact {
	if e == nil {
		return nil
	}
	return e.Artifacts
}

func (e *ExecDetails) GetStatus() models.PlaybookActionStatus {
	if e.ExitCode != 0 {
		return models.PlaybookActionStatusFailed
	}
	return models.PlaybookActionStatusCompleted
}

func (c *ExecAction) Run(ctx context.Context, exec v1.ExecAction) (*ExecDetails, error) {
	envParams, err := c.prepareEnvironment(ctx, exec)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	cmd, err := CreateCommandFromScript(ctx, exec.Script)
	if err != nil {
		return nil, ctx.Oops().With("script", exec.Script).Wrap(err)
	}

	if len(envParams.envs) != 0 {
		cmd.Env = append(os.Environ(), envParams.envs...)
	}
	if envParams.mountPoint != "" {
		cmd.Dir = envParams.mountPoint
	}

	if cleanup, err := connection.SetupConnection(ctx, exec.Connections, cmd); err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to setup connection")
	} else {
		defer func() {
			if err := cleanup(); err != nil {
				ctx.Errorf("something went wrong cleaning up connection artifacts: %v", err)
			}
		}()
	}

	return runCmd(ctx, cmd, exec.Artifacts...)
}

func runCmd(ctx context.Context, cmd *osExec.Cmd, artifactConfigs ...v1.Artifact) (*ExecDetails, error) {
	var (
		result ExecDetails
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result.Error = cmd.Run()
	result.Args = cmd.Args
	result.Path = cmd.Path
	result.ExitCode = cmd.ProcessState.ExitCode()
	result.Stderr = strings.TrimSpace(stderr.String())
	result.Stdout = strings.TrimSpace(stdout.String())

	ctx.Logger.V(3).Infof("command exited with code %d and stdout=%d bytes, stderr=%d bytes", result.ExitCode, len(result.Stdout), len(result.Stderr))

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
				file, err := os.Open(path)
				if err != nil {
					return nil, fmt.Errorf("error opening artifact file. path=%s; %w", path, err)
				}

				if stat, err := file.Stat(); err != nil {
					return nil, fmt.Errorf("error getting artifact file stat. path=%s; %w", path, err)
				} else if stat.IsDir() {
					return nil, fmt.Errorf("artifact path (%s) is a directory. expected file", path)
				}

				result.Artifacts = append(result.Artifacts, artifacts.Artifact{
					Content: file,
					Path:    path,
				})
			}
		}
	}

	return &result, nil
}

type execEnv struct {
	envs       []string
	mountPoint string
}

func (c *ExecAction) prepareEnvironment(ctx context.Context, check v1.ExecAction) (*execEnv, error) {
	var result execEnv

	for _, env := range check.EnvVars {
		val, err := ctx.GetEnvValueFromCache(env, ctx.GetNamespace())
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
