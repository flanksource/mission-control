package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

var Playbook = &cobra.Command{
	Use: "playbook",
}

var playbookNamespace string
var paramFile string
var debugPort int

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

		shutdown.AddHook(stop)

		e := echo.New(ctx)

		shutdown.AddHook(func() {
			echo.Shutdown(e)
		})

		shutdown.AddHook(func() {
			for k, v := range logger.GetNamedLoggingLevels() {
				logger.Infof("logger: %s=%v", k, v)
			}

			for k, v := range properties.Global.GetAll() {
				logger.Infof("property: %s=%v", k, v)
			}
		})
		shutdown.WaitForSignal()

		if debugPort >= 0 {
			if debugPort == 0 {
				debugPort = duty.FreePort()
			}
			go echo.Start(e, debugPort)
		}

		p, err := playbook.CreateOrSaveFromFile(ctx, args[0])
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}

		var params = playbook.RunParams{Params: make(map[string]string)}

		if f, err := os.Open(paramFile); err == nil {
			if err := yamlutil.NewYAMLOrJSONDecoder(f, 1024).Decode(&params); err != nil {
				logger.Fatalf(err.Error())
				return
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

		ctx = ctx.WithUser(auth.GetSystemUser(&ctx))
		run, err := playbook.Run(ctx, p, params)
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}

		ctx = ctx.WithObject(p, run)

		ctx = ctx.WithNamespace(lo.CoalesceOrEmpty(p.Namespace, api.Namespace))

		var action *v1.PlaybookAction

		action, err = runner.GetNextActionToRun(ctx, *p, *run)
		if err != nil {
			logger.Fatalf(err.Error())
			return
		}

		for action != nil {

			runAction, err := run.StartAction(ctx.DB(), action.Name)

			if err != nil {
				ctx.Errorf("Error starting action %s: %v", action.Name, err)
				break
			}

			if err = runner.RunAction(ctx, run, runAction); err != nil {
				ctx.Errorf("Error running action %s: %v", action.Name, err)
				break
			}

			action, err = runner.GetNextActionToRun(ctx, *p, *run)

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

		b, _ := json.MarshalIndent(summary, "", " ")
		fmt.Println(string(b))
		if err != nil {
			shutdown.ShutdownAndExit(1, err.Error())
		}

		if summary.Run.Status != models.PlaybookRunStatusCompleted {
			shutdown.ShutdownAndExit(1, fmt.Sprintf("Playbook run status: %s ", summary.Run.Status))
		}

	},
}

var Submit = &cobra.Command{
	Use:              "submit playbook playbook.yaml params.yaml",
	Args:             cobra.ExactArgs(1),
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

		shutdown.AddHook(stop)

		shutdown.WaitForSignal()

		p, err := playbook.CreateOrSaveFromFile(ctx, args[0])
		if err != nil {
			return err
		}

		var params playbook.RunParams

		if f, err := os.Open(paramFile); err == nil {
			if err := yamlutil.NewYAMLOrJSONDecoder(f, 1024).Decode(&params); err != nil {
				return err
			}
		}

		run, err := playbook.Run(ctx, p, params)
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
	Run.Flags().IntVar(&debugPort, "debug-port", 0, "Start an HTTP server to use the /debug routes, Use -1 to disable and 0 to pick a free port")
	Playbook.AddCommand(Run, Submit)
	Root.AddCommand(Playbook)
}
