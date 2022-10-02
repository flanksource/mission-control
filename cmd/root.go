package cmd

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var Root = &cobra.Command{
	Use: "incident-commander",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.UseZap(cmd.Flags())
	},
}

var dev bool
var httpPort, metricsPort, devGuiPort int
var publicEndpoint = "http://localhost:8080"
var apmHub, configDb, canaryChecker, kratosAPI string

func ServerFlags(flags *pflag.FlagSet) {
	flags.IntVar(&httpPort, "httpPort", 8080, "Port to expose a health dashboard ")
	flags.IntVar(&devGuiPort, "devGuiPort", 3004, "Port used by a local npm server in development mode")
	flags.IntVar(&metricsPort, "metricsPort", 8081, "Port to expose a health dashboard ")
	flags.BoolVar(&dev, "dev", false, "Run in development mode")
	flags.StringVar(&publicEndpoint, "public-endpoint", "http://localhost:8080", "Public endpoint that this instance is exposed under")
	flags.StringVar(&apmHub, "apm-hub", "apm-hub", "APM Hub URL")
	flags.StringVar(&configDb, "config-db", "config-db", "Config DB URL")
	flags.StringVar(&canaryChecker, "canary-checker", "canary-checker", "Canary Checker URL")
	flags.StringVar(&kratosAPI, "kratos-api", "kratos-public", "Kratos API service")
}

func init() {
	logger.BindFlags(Root.PersistentFlags())
	db.Flags(Root.PersistentFlags())
	Root.AddCommand(Serve, GoOffline)
}
