package cmd

import (
	"os"
	"strconv"
	"strings"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/k8s"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/telemetry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/kubernetes/fake"
)

func PreRun(cmd *cobra.Command, args []string) {
	if err := db.Init(db.ConnectionString); err != nil {
		logger.Fatalf("Failed to initialize the db: %v", err)
	}

	if isPartial, err := api.UpstreamConf.IsPartiallyFilled(); isPartial && err != nil {
		logger.Warnf("Please ensure that all the required flags for upstream is supplied: %v", err)
	} else if !isPartial {
		api.UpstreamConf.Options = append(api.UpstreamConf.Options, func(c *http.Client) {
			c.UserAgent(api.BuildVersion)
		})
	}

	var err error
	api.Kubernetes, err = k8s.NewClient()
	if err != nil {
		logger.Infof("Kubernetes client not available: %v", err)
		api.Kubernetes = fake.NewSimpleClientset()
	}

	if otelcollectorURL != "" {
		resourceAttrs := []attribute.KeyValue{}
		if auth.AuthMode == auth.Clerk && auth.ClerkOrgID != "" {
			resourceAttrs = append(resourceAttrs, attribute.String("org.id", auth.ClerkOrgID))
		}
		telemetry.InitTracer(otelServiceName, otelcollectorURL, true, resourceAttrs)
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
		logger.UseZap()
	},
}

var (
	dev                               bool
	httpPort, metricsPort, devGuiPort int
	configDb, postgrestURI            string
	disablePostgrest                  bool

	// disableKubernetes is used to run mission-control on a non-operator mode.
	disableKubernetes bool

	// Telemetry flag vars
	otelcollectorURL string
	otelServiceName  string
)

func ServerFlags(flags *pflag.FlagSet) {
	flags.IntVar(&httpPort, "httpPort", 8080, "Port to expose a health dashboard")
	flags.StringVar(&api.Namespace, "namespace", utils.Coalesce(os.Getenv("NAMESPACE"), "default"), "Namespace to use for config/secret lookups")
	flags.IntVar(&devGuiPort, "devGuiPort", 3004, "Port used by a local npm server in development mode")
	flags.IntVar(&metricsPort, "metricsPort", 8081, "Port to expose a health dashboard ")
	flags.BoolVar(&dev, "dev", false, "Run in development mode")
	flags.StringVar(&api.PublicWebURL, "public-endpoint", "http://localhost:3000", "Public endpoint this instance is exposed under")
	flags.StringVar(&api.ApmHubPath, "apm-hub", "http://apm-hub:8080", "APM Hub URL")
	flags.StringVar(&configDb, "config-db", "http://config-db:8080", "Config DB URL")
	flags.StringVar(&auth.KratosAPI, "kratos-api", "http://kratos-public:80", "Kratos API service")
	flags.StringVar(&auth.KratosAdminAPI, "kratos-admin", "http://kratos-admin:80", "Kratos Admin API service")
	flags.StringVar(&auth.ClerkJWKSURL, "clerk-jwks-url", "", "Clerk JWKS URL")
	flags.StringVar(&auth.ClerkOrgID, "clerk-org-id", "", "Clerk Organization ID")
	flags.StringVar(&postgrestURI, "postgrest-uri", "http://localhost:3000", "URL for the PostgREST instance to use. If localhost is supplied, a PostgREST instance will be started")
	flags.StringVar(&auth.AuthMode, "auth", "", "Enable authentication via Kratos or Clerk. Valid values are [kratos, clerk]")
	flags.BoolVar(&disablePostgrest, "disable-postgrest", false, "Disable PostgREST. Deprecated (Use --postgrest-uri '' to disable PostgREST)")
	flags.BoolVar(&disableKubernetes, "disable-kubernetes", false, "Disable Kubernetes (non-operator mode)")
	flags.StringVar(&mail.FromAddress, "email-from-address", "no-reply@flanksource.com", "Email address of the sender")
	flags.StringVar(&mail.FromName, "email-from-name", "Mission Control", "Email name of the sender")
	flags.StringVar(&db.PostgresDBAnonRole, "postgrest-anon-role", "postgrest_anon", "PostgREST anonymous role")
	flags.StringVar(&db.PostgrestMaxRows, "postgrest-max-rows", "2000", "A hard limit to the number of rows PostgREST will fetch")
	flags.StringVar(&auth.IdentityRoleMapper, "identity-role-mapper", "", "CEL-Go expression to map identity to a role & a team (return: {role: string, teams: []string}). Supports file path (prefixed with 'file://').")
	flags.StringVar(&otelcollectorURL, "otel-collector-url", "", "OpenTelemetry gRPC Collector URL in host:port format")
	flags.StringVar(&otelServiceName, "otel-service-name", "mission-control", "OpenTelemetry service name for the resource")
	flags.StringVar(&api.DefaultArtifactConnection, "artifact-connection", "", "Specify the default connection to use for artifacts (can be the connection string or the connection id)")

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
	logger.BindFlags(Root.PersistentFlags())
	duty.BindFlags(Root.PersistentFlags())

	db.Flags(Root.PersistentFlags())
	Root.PersistentFlags().StringVar(&api.CanaryCheckerPath, "canary-checker", "http://canary-checker:8080", "Canary Checker URL")
	Root.AddCommand(Serve, Sync, GoOffline)
}
