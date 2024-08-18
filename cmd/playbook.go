package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/playbook/runner"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

var Playbook = &cobra.Command{
	Use: "playbook",
}

var playbookNamespace string
var paramFile string

var Run = &cobra.Command{
	Use:              "run playbook playbook.yaml params.yaml",
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

		AddShutdownHook(stop)

		e := echo.New(ctx)

		AddShutdownHook(func() {
			echo.Shutdown(e)
		})

		go echo.Start(e, httpPort)

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

		ctx = ctx.WithObject(p, run)

		ctx = ctx.WithNamespace(lo.CoalesceOrEmpty(p.Namespace, api.Namespace))

		var action *v1.PlaybookAction

		action, err = runner.GetNextActionToRun(ctx, *p, *run)
		if err != nil {
			return err
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
				ShutdownAndExit(1, "")
			}
			if err != nil {
				ctx.Errorf("Error getting next action %s: %v", action.Name, err)
				break
			}
		}

		summary, err := playbook.GetPlaybookStatus(ctx, run.ID)

		b, _ := json.MarshalIndent(summary, "", " ")
		fmt.Println(string(b))
		if err != nil || summary.Run.Status != models.PlaybookRunStatusCompleted {
			ShutdownAndExit(1, "")
		}
		Shutdown()
		return nil
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
		defer Shutdown()

		AddShutdownHook(stop)

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
	Playbook.AddCommand(Run, Submit)
	Root.AddCommand(Playbook)
}
