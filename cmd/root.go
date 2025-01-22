package cmd

import (
	"os"
	"strconv"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/secret"
	"github.com/flanksource/duty/telemetry"
	"go.opentelemetry.io/otel/attribute"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/vars"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func PreRun(cmd *cobra.Command, args []string) {
	if isPartial, err := api.UpstreamConf.IsPartiallyFilled(); isPartial && err != nil {
		logger.Warnf("Please ensure that all the required flags for upstream is supplied: %v", err)
	}

	if vars.AuthMode == auth.Clerk && auth.ClerkOrgID != "" {
		telemetry.OtelAttributes = append(telemetry.OtelAttributes, attribute.String("org.id", auth.ClerkOrgID))
	}

	if strings.HasPrefix(auth.IdentityRoleMapper, "file://") {
		path := strings.TrimPrefix(auth.IdentityRoleMapper, "file://")
		content, err := os.ReadFile(path)
		if err != nil {
			logger.Fatalf("failed to read identity role mapper script file(%s): %v", path, err)
		}

		auth.IdentityRoleMapper = string(content)
		logger.Debugf("successfully loaded identity-role-mapper from file: %s", auth.IdentityRoleMapper)
	}
}

var Root = &cobra.Command{
	Use: "incident-commander",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.UseSlog()
		if err := properties.LoadFile("mission-control.properties"); err != nil {
			logger.Errorf(err.Error())
		}
		dutyApi.DefaultConfig.SkipMigrationFiles = []string{"012_changelog.sql"}
		dutyApi.DefaultConfig = dutyApi.DefaultConfig.ReadEnv()
	},
}

var (
	dev                               bool
	httpPort, metricsPort, devGuiPort int

	// disableKubernetes is used to run mission-control on a non-operator mode.
	disableKubernetes bool
	disableOperators  bool
)

func ServerFlags(flags *pflag.FlagSet) {
	duty.BindPFlags(flags)
	flags.IntVar(&httpPort, "httpPort", 8080, "Server port")
	flags.StringVar(&api.Namespace, "namespace", utils.Coalesce(os.Getenv("NAMESPACE"), "default"), "Namespace to use for config/secret lookups")
	flags.IntVar(&devGuiPort, "devGuiPort", 3004, "Port used by a local npm server in development mode")
	flags.IntVar(&metricsPort, "metricsPort", 8081, "Port to expose a health dashboard ")
	flags.BoolVar(&dev, "dev", false, "Run in development mode")
	flags.BoolVar(&disableOperators, "disable-operators", false, "Disable Kubernetes Operators")
	flags.StringVar(&api.FrontendURL, "frontend-url", "http://localhost:3000", "URL of the frontend")
	flags.StringVar(&api.PublicURL, "public-endpoint", "http://localhost:8080", "Public endpoint this instance is exposed under")
	flags.StringVar(&api.ApmHubPath, "apm-hub", "http://apm-hub:8080", "APM Hub URL")
	flags.StringVar(&api.ConfigDB, "config-db", "http://config-db:8080", "Config DB URL")
	flags.StringVar(&auth.KratosAPI, "kratos-api", "http://kratos-public:80", "Kratos API service")
	flags.StringVar(&auth.KratosAdminAPI, "kratos-admin", "http://kratos-admin:80", "Kratos Admin API service")
	flags.StringVar(&auth.ClerkJwksUrl, "clerk-jwks-url", "", "Clerk JWKS URL")
	flags.StringVar(&auth.ClerkOrgID, "clerk-org-id", "", "Clerk Organization ID")
	flags.StringVar(&vars.AuthMode, "auth", "", "Enable authentication via Kratos or Clerk. Valid values are [kratos, clerk, basic]")
	flags.StringVar(&auth.HtpasswdFile, "htpasswd-file", "htpasswd", "Path to htpasswd file for basic authentication")
	flags.StringVar(&mail.FromAddress, "email-from-address", "no-reply@flanksource.com", "Email address of the sender")
	flags.StringVar(&mail.FromName, "email-from-name", "Mission Control", "Email name of the sender")
	flags.StringSliceVar(&echo.AllowedCORS, "allowed-cors", []string{"https://app.flanksource.com", "https://beta.flanksource.com"}, "Allowed CORS credential origins")
	flags.StringVar(&auth.IdentityRoleMapper, "identity-role-mapper", "", "CEL-Go expression to map identity to a role & a team (return: {role: string, teams: []string}). Supports file path (prefixed with 'file://').")
	flags.StringVar(&api.DefaultArtifactConnection, "artifact-connection", "", "Specify the default connection to use for artifacts (can be the connection string or the connection id)")
	flags.StringVar(&secret.KMSConnection, "secret-keeper-connection", "", "Specify the connection to use for secret keepers (can be the connection string or the connection id)")

	var upstreamPageSizeDefault = 500
	if val, exists := os.LookupEnv("UPSTREAM_PAGE_SIZE"); exists {
		if parsed, err := strconv.Atoi(val); err != nil {
			logger.Fatalf("invalid value=%s for UPSTREAM_PAGE_SIZE: %v", val, err)
		} else {
			upstreamPageSizeDefault = parsed
		}
	}

	var upstreamUserDefault = "token"
	if val, exists := os.LookupEnv("UPSTREAM_USER"); exists {
		upstreamUserDefault = val
	}

	// Flags for upstream push
	flags.StringVar(&api.UpstreamConf.Host, "upstream-host", os.Getenv("UPSTREAM_HOST"), "URL for Mission Control central instance")
	flags.StringVar(&api.UpstreamConf.Username, "upstream-user", upstreamUserDefault, "upstream username")
	flags.StringVar(&api.UpstreamConf.Password, "upstream-password", os.Getenv("UPSTREAM_PASSWORD"), "upstream password")
	flags.StringVar(&api.UpstreamConf.AgentName, "upstream-agent-name", os.Getenv("AGENT_NAME"), "name of the cluster")
	flags.StringSliceVar(&api.UpstreamConf.Labels, "upstream-labels", strings.Split(os.Getenv("UPSTREAM_LABELS"), ","), `labels in the format: "key1=value1,key2=value2"`)
	flags.IntVar(&jobs.ReconcilePageSize, "upstream-page-size", upstreamPageSizeDefault, "upstream reconciliation page size")
}

func init() {
	// http.DefaultUserAgent = api.BuildVersion
	logger.BindFlags(Root.PersistentFlags())
	properties.BindFlags(Root.PersistentFlags())
	telemetry.BindFlags(Root.PersistentFlags(), "mission-control")

	Root.PersistentFlags().StringVar(&api.CanaryCheckerPath, "canary-checker", "http://canary-checker:8080", "Canary Checker URL")
	Root.AddCommand(Serve, Sync, GoOffline)
}
