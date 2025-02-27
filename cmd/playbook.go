package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

var Playbook = &cobra.Command{
	Use: "playbook",
}

var playbookNamespace string
var outfile string
var outFormat string
var paramFile string
var debugPort int

func GetOrCreateAgent(ctx context.Context, name string) (*models.Agent, error) {
	var t models.Agent
	if err := ctx.DB().Where("name = ?", name).First(&t).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if t.ID != uuid.Nil {
		return &t, nil
	}

	t = models.Agent{Name: name}
	tx := ctx.DB().Create(&t)
	return &t, tx.Error
}

func parsePlaybookArgs(ctx context.Context, args []string) (*models.Playbook, *playbook.RunParams, error) {
	p, err := playbook.CreateOrSaveFromFile(ctx, args[0])
	if err != nil {
		return nil, nil, err
	}

	hostname, _ := os.Hostname()
	agent, err := GetOrCreateAgent(ctx, hostname)
	if err != nil {
		return nil, nil, err
	}

	var params = playbook.RunParams{
		Params:  make(map[string]string),
		AgentID: &agent.ID,
	}

	if f, err := os.Open(paramFile); err == nil {
		if err := yamlutil.NewYAMLOrJSONDecoder(f, 1024).Decode(&params.Params); err != nil {
			return nil, nil, err
		}
	}

	for _, arg := range args[1:] {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			logger.Warnf("Invalid param: %s", arg)
			continue
		}
		if parts[0] == "config" || parts[0] == "config_id" {
			params.ConfigID = lo.ToPtr(uuid.MustParse(parts[1]))
		} else if parts[0] == "component" || parts[0] == "component_id" {
			params.ComponentID = lo.ToPtr(uuid.MustParse(parts[1]))
		} else if parts[0] == "check" || parts[0] == "check_id" {
			params.CheckID = lo.ToPtr(uuid.MustParse(parts[1]))
		} else {
			params.Params[parts[0]] = parts[1]
		}
	}
	return p, &params, nil
}

var Run = &cobra.Command{
	Use:              "run playbook playbook.yaml params.yaml",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}

		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

		if debugPort >= 0 {
			e := echo.New(ctx)

			shutdown.AddHookWithPriority("echo", shutdown.PriorityIngress, func() {
				echo.Shutdown(e)
			})

			if debugPort == 0 {
				debugPort = duty.FreePort()
			}
			go echo.Start(e, debugPort)
		}
		shutdown.WaitForSignal()

		p, params, err := parsePlaybookArgs(ctx, args)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}

		sysUser, err := db.GetSystemUser(ctx)
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}
		ctx = ctx.WithUser(sysUser)
		run, err := playbook.Run(ctx, p, *params)
		if err != nil {
			logger.Errorf("%+v", err)
			shutdown.ShutdownAndExit(1, err.Error())
			return
		}

		ctx = ctx.WithObject(p, run)

		ctx = ctx.WithNamespace(lo.CoalesceOrEmpty(p.Namespace, api.Namespace))

		var action *v1.PlaybookAction
		var step *models.PlaybookRunAction

		action, step, err = runner.GetNextActionToRun(ctx, *run)
		if err != nil {
			logger.Fatalf(err.Error())
			shutdown.ShutdownAndExit(1, err.Error())
			return
		}

		if action == nil {
			logger.Errorf("No actions to run")
			shutdown.ShutdownAndExit(1, err.Error())
			return
		}

		for action != nil {
			if delayed, err := runner.CheckDelay(ctx, *p, *run, action, step); err != nil {
				ctx.Errorf("Error running action %s: %v", action.Name, err)
				break
			} else if delayed {
				break
			}

			runAction, err := run.StartAction(ctx.DB(), action.Name)
			if err != nil {
				ctx.Errorf("Error starting action %s: %v", action.Name, err)
				break
			}

			if err = runner.RunAction(ctx, run, runAction); err != nil {
				ctx.Errorf("Error running action %s: %v", action.Name, err)
				break
			}

			action, _, err = runner.GetNextActionToRun(ctx, *run)
			if action != nil && action.Name == runAction.Name {
				ctx.Errorf("%v", ctx.Oops().Errorf("Action cycle detected for: %s", action.Name))
				shutdown.ShutdownAndExit(1, "")
			}

			if err != nil {
				ctx.Errorf("Error getting next action %s: %v", action.Name, err)
				break
			}
		}

		summary, err := playbook.GetPlaybookStatus(ctx, run.ID)

		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}

		saveOutput(summary, outfile, outFormat)

		if summary.Run.Status != models.PlaybookRunStatusCompleted {
			shutdown.ShutdownAndExit(1, fmt.Sprintf("Playbook run status: %s ", summary.Run.Status))
		}
	},
}

func saveOutput(object any, file string, format string) {
	var out string
	if format == "yaml" {
		b, _ := yaml.Marshal(object)
		out = string(b)
	} else {
		b, _ := json.MarshalIndent(object, "", "  ")
		out = string(b)
	}

	if file != "" {
		_ = os.WriteFile(file, []byte(out), 0600)
	} else {
		fmt.Println(out)
	}
}

var Submit = &cobra.Command{
	Use:              "submit playbook playbook.yaml params.yaml",
	Args:             cobra.MinimumNArgs(1),
	PersistentPreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
		if err != nil {
			return err
		}

		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

		shutdown.WaitForSignal()

		p, params, err := parsePlaybookArgs(ctx, args)
		params.AgentID = nil
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}

		sysUser, err := db.GetSystemUser(ctx)
		if err != nil {
			logger.Fatalf(err.Error())
			os.Exit(1)
		}
		ctx = ctx.WithUser(sysUser)

		run, err := playbook.Run(ctx, p, *params)
		if err != nil {
			return err
		}

		fmt.Println(logger.Pretty(run))

		return nil
	},
}

func init() {
	Playbook.PersistentFlags().StringVarP(&playbookNamespace, "namespace", "n", "default", "Namespace for playbook to run under")
	Playbook.PersistentFlags().StringVarP(&paramFile, "params", "p", "", "YAML/JSON file containing parameters")
	Run.Flags().IntVar(&debugPort, "debug-port", -1, "Start an HTTP server to use the /debug routes, Use -1 to disable and 0 to pick a free port")
	Run.Flags().StringVarP(&outfile, "out-file", "o", "", "Write playbook summary to file instead of stdout")
	Run.Flags().StringVarP(&outFormat, "out-format", "f", "yaml", "Format of output file or stdout (yaml or json)")

	Playbook.AddCommand(Run, Submit)
	Root.AddCommand(Playbook)
}
