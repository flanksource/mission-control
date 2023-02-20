package cmd

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/rules"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func PreRun(cmd *cobra.Command, args []string) {
	if err := db.Init(db.ConnectionString); err != nil {
		logger.Fatalf("Failed to initialize the db: %v", err)
	}
}

var Root = &cobra.Command{
	Use: "incident-commander",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.UseZap(cmd.Flags())
	},
}

var dev bool
var httpPort, metricsPort, devGuiPort int
var publicEndpoint = "http://localhost:8080"
var configDb, kratosAPI, kratosAdminAPI, externalPostgrestUri string
var enableAuth, disablePostgrest bool
var upstreamConfig api.UpstreamConfig

func ServerFlags(flags *pflag.FlagSet) {
	flags.IntVar(&httpPort, "httpPort", 8080, "Port to expose a health dashboard ")
	flags.IntVar(&devGuiPort, "devGuiPort", 3004, "Port used by a local npm server in development mode")
	flags.IntVar(&metricsPort, "metricsPort", 8081, "Port to expose a health dashboard ")
	flags.BoolVar(&dev, "dev", false, "Run in development mode")
	flags.StringVar(&publicEndpoint, "public-endpoint", "http://localhost:8080", "Public endpoint that this instance is exposed under")
	flags.StringVar(&api.ApmHubPath, "apm-hub", "http://apm-hub:8080", "APM Hub URL")
	flags.StringVar(&configDb, "config-db", "http://config-db:8080", "Config DB URL")
	flags.StringVar(&kratosAPI, "kratos-api", "http://kratos-public:80", "Kratos API service")
	flags.StringVar(&kratosAdminAPI, "kratos-admin", "http://kratos-admin:80", "Kratos Admin API service")
	flags.StringVar(&externalPostgrestUri, "external-postgrest-uri", "", "URI for the postgrest instance to use")
	flags.BoolVar(&enableAuth, "enable-auth", false, "Enable authentication via Kratos")
	flags.DurationVar(&rules.Period, "rules-period", 5*time.Minute, "Period to run the rules")
	flags.BoolVar(&disablePostgrest, "disable-postgrest", false, "Disable postgrest")
	flags.StringVar(&mail.FromAddress, "email-from-address", "no-reply@flanksource.com", "Email address of the sender")
	flags.StringVar(&db.PostgresDBAnonRole, "postgrest-anon-role", "postgrest_anon", "Postgres anonymous role")

	// Flags for upstream push
	flags.StringVar(&upstreamConfig.URL, "upstream-url", "", "central incident commander instance to push configs to")
	flags.StringVar(&upstreamConfig.Username, "upstream-user", "", "upstream username")
	flags.StringVar(&upstreamConfig.Password, "upstream-password", "", "upstream password")
	flags.StringSliceVar(&upstreamConfig.Labels, "upstream-labels", nil, `labels. example: "cluster=a,cluster=b"`)
}

func init() {
	logger.BindFlags(Root.PersistentFlags())
	db.Flags(Root.PersistentFlags())
	Root.PersistentFlags().StringVar(&api.CanaryCheckerPath, "canary-checker", "http://canary-checker:8080", "Canary Checker URL")
	Root.AddCommand(Serve, Run, Sync, GoOffline)
}
