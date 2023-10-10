package cmd

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/k8s"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/rules"
	"github.com/flanksource/incident-commander/telemetry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes/fake"
)

func PreRun(cmd *cobra.Command, args []string) {
	if err := db.Init(db.ConnectionString); err != nil {
		logger.Fatalf("Failed to initialize the db: %v", err)
	}

	var err error
	api.Kubernetes, err = k8s.NewClient()
	if err != nil {
		logger.Infof("Kubernetes client not available: %v", err)
		api.Kubernetes = fake.NewSimpleClientset()
	}

	api.DefaultContext = api.NewContext(db.Gorm, db.Pool)

	if otelcollectorURL != "" {
		telemetry.InitTracer("mission-control", otelcollectorURL, true)
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
var configDb, authMode, kratosAPI, kratosAdminAPI, postgrestURI string
var clerkJWKSURL, clerkOrgID string
var disablePostgrest bool
var otelcollectorURL string

func ServerFlags(flags *pflag.FlagSet) {
	flags.IntVar(&httpPort, "httpPort", 8080, "Port to expose a health dashboard")
	flags.StringVar(&api.Namespace, "namespace", os.Getenv("NAMESPACE"), "Namespace to use for config/secret lookups")
	flags.IntVar(&devGuiPort, "devGuiPort", 3004, "Port used by a local npm server in development mode")
	flags.IntVar(&metricsPort, "metricsPort", 8081, "Port to expose a health dashboard ")
	flags.BoolVar(&dev, "dev", false, "Run in development mode")
	flags.StringVar(&api.PublicWebURL, "public-endpoint", "http://localhost:3000", "Public endpoint this instance is exposed under")
	flags.StringVar(&api.ApmHubPath, "apm-hub", "http://apm-hub:8080", "APM Hub URL")
	flags.StringVar(&configDb, "config-db", "http://config-db:8080", "Config DB URL")
	flags.StringVar(&kratosAPI, "kratos-api", "http://kratos-public:80", "Kratos API service")
	flags.StringVar(&kratosAdminAPI, "kratos-admin", "http://kratos-admin:80", "Kratos Admin API service")
	flags.StringVar(&clerkJWKSURL, "clerk-jwks-url", "", "Clerk JWKS URL")
	flags.StringVar(&clerkOrgID, "clerk-org-id", "", "Clerk Organization ID")
	flags.StringVar(&postgrestURI, "postgrest-uri", "http://localhost:3000", "URL for the PostgREST instance to use. If localhost is supplied, a PostgREST instance will be started")
	flags.StringVar(&authMode, "auth", "", "Enable authentication via Kratos or Clerk. Valid values are [kratos, clerk]")
	flags.DurationVar(&rules.Period, "rules-period", 5*time.Minute, "Period to run the rules")
	flags.BoolVar(&disablePostgrest, "disable-postgrest", false, "Disable PostgREST. Deprecated (Use --postgrest-uri '' to disable PostgREST)")
	flags.StringVar(&mail.FromAddress, "email-from-address", "no-reply@flanksource.com", "Email address of the sender")
	flags.StringVar(&mail.FromName, "email-from-name", "Mission Control", "Email name of the sender")
	flags.StringVar(&db.PostgresDBAnonRole, "postgrest-anon-role", "postgrest_anon", "PostgREST anonymous role")
	flags.StringVar(&db.PostgrestMaxRows, "postgrest-max-rows", "2000", "A hard limit to the number of rows PostgREST will fetch")
	flags.StringVar(&otelcollectorURL, "otel-collector-url", "", "OpenTelemetry gRPC Collector URL in host:port format")

	var upstreamPageSizeDefault = 500
	if val, exists := os.LookupEnv("UPSTREAM_PAGE_SIZE"); exists {
		if parsed, err := strconv.Atoi(val); err != nil {
			logger.Fatalf("invalid value=%s for UPSTREAM_PAGE_SIZE: %v", val, err)
		} else {
			upstreamPageSizeDefault = parsed
		}
	}

	var upstreamMaxAgeDefault = time.Hour * 48
	if val, exists := os.LookupEnv("UPSTREAM_MAX_AGE"); exists {
		if parsed, err := time.ParseDuration(val); err != nil {
			logger.Fatalf("invalid value=%s for UPSTREAM_MAX_AGE: %v", val, err)
		} else {
			upstreamMaxAgeDefault = parsed
		}
	}

	// Flags for upstream push
	flags.StringVar(&api.UpstreamConf.Host, "upstream-host", os.Getenv("UPSTREAM_HOST"), "URL for Mission Control central instance")
	flags.StringVar(&api.UpstreamConf.Username, "upstream-user", os.Getenv("UPSTREAM_USER"), "upstream username")
	flags.StringVar(&api.UpstreamConf.Password, "upstream-password", os.Getenv("UPSTREAM_PASSWORD"), "upstream password")
	flags.StringVar(&api.UpstreamConf.AgentName, "upstream-name", os.Getenv("UPSTREAM_NAME"), "name of the cluster")
	flags.StringSliceVar(&api.UpstreamConf.Labels, "upstream-labels", strings.Split(os.Getenv("UPSTREAM_LABELS"), ","), `labels in the format: "key1=value1,key2=value2"`)
	flags.IntVar(&jobs.ReconcilePageSize, "upstream-page-size", upstreamPageSizeDefault, "upstream reconciliation page size")
	flags.DurationVar(&jobs.ReconcileMaxAge, "upstream-max-age", upstreamMaxAgeDefault, "upstream reconciliation max age")
}

func init() {
	logger.BindFlags(Root.PersistentFlags())
	db.Flags(Root.PersistentFlags())
	Root.PersistentFlags().StringVar(&api.CanaryCheckerPath, "canary-checker", "http://canary-checker:8080", "Canary Checker URL")
	Root.AddCommand(Serve, Run, Sync, GoOffline)
}
