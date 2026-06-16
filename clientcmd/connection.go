package clientcmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// LocalConnectionOps provides the database/credential-backed connection
// operations that the full mission-control binary supports. The slim faro
// client leaves this nil and operates exclusively against a remote server.
type LocalConnectionOps interface {
	LoadAWSProfile(flags *ConnectionFlags) error
	AddViaDB(flags *ConnectionFlags, conn *models.Connection) error
	TestSaved(name, namespace string, overrides *ConnectionFlags) (any, error)
	TestTransient(flags *ConnectionFlags) (any, error)
	TestFile(filename string) (any, error)
	GetConnection(name, namespace string) (*models.Connection, error)
	SaveConnection(conn *models.Connection) error
}

// browserGetConnection loads a connection remotely when an API context is set,
// otherwise via the local DB implementation.
func browserGetConnection(name, namespace string) (*models.Connection, error) {
	if mc, ok := ContextHasAPI(); ok {
		return NewAPIClient(mc).GetConnection(name, namespace)
	}
	if LocalConnections == nil {
		return nil, errNoLocalConnections
	}
	return LocalConnections.GetConnection(name, namespace)
}

// browserSaveConnection saves a connection remotely when an API context is set,
// otherwise via the local DB implementation.
func browserSaveConnection(conn *models.Connection) error {
	if mc, ok := ContextHasAPI(); ok {
		return NewAPIClient(mc).SaveConnection(conn)
	}
	if LocalConnections == nil {
		return errNoLocalConnections
	}
	return LocalConnections.SaveConnection(conn)
}

// LocalConnections is set by the full binary to enable local DB/credential
// operations. nil in faro.
var LocalConnections LocalConnectionOps

var errNoLocalConnections = fmt.Errorf("this operation requires a Mission Control server context (run `auth login` / `context add --server`) or the full mission-control binary")

var Connection = &cobra.Command{
	Use:          "connection",
	Short:        "Manage connections",
	SilenceUsage: true,
}

var ConnectionAdd = &cobra.Command{
	Use:   "add",
	Short: "Add a new connection",
	Long: `Add a new connection to the database.

Examples:
  app connection add slack --name test --namespace default --token xoxb-xxx --channel C12345
  app connection add postgres --name mydb --namespace default --url "postgres://user:pass@localhost:5432/db"
  app connection add aws --name my-aws --namespace default --access-key AKIA... --secret-key ... --region us-east-1
  app connection add postgres --name mydb --namespace default --url "..." --test`,
	SilenceUsage:      true,
	DisableAutoGenTag: true,
}

type connectionTypeSpec struct {
	Name    string
	Type    string
	Aliases []string
	Short   string
}

var connectionAddTypeSpecs = []connectionTypeSpec{
	{Name: "slack", Type: models.ConnectionTypeSlack, Short: "Add a Slack connection"},
	{Name: "postgres", Type: models.ConnectionTypePostgres, Short: "Add a Postgres connection"},
	{Name: "mysql", Type: models.ConnectionTypeMySQL, Short: "Add a MySQL connection"},
	{Name: "mssql", Type: models.ConnectionTypeSQLServer, Aliases: []string{"sqlserver", "sql-server"}, Short: "Add a SQL Server connection"},
	{Name: "mongo", Type: models.ConnectionTypeMongo, Aliases: []string{"mongodb"}, Short: "Add a MongoDB connection"},
	{Name: "aws", Type: models.ConnectionTypeAWS, Short: "Add an AWS connection"},
	{Name: "aws-kms", Type: models.ConnectionTypeAWSKMS, Aliases: []string{"awskms"}, Short: "Add an AWS KMS connection"},
	{Name: "s3", Type: models.ConnectionTypeS3, Short: "Add an S3 connection"},
	{Name: "azure", Type: models.ConnectionTypeAzure, Short: "Add an Azure connection"},
	{Name: "azure-keyvault", Type: models.ConnectionTypeAzureKeyVault, Aliases: []string{"azure-key-vault"}, Short: "Add an Azure Key Vault connection"},
	{Name: "azure-devops", Type: models.ConnectionTypeAzureDevops, Aliases: []string{"azuredevops"}, Short: "Add an Azure DevOps connection"},
	{Name: "gcp", Type: models.ConnectionTypeGCP, Short: "Add a GCP connection"},
	{Name: "gcs", Type: models.ConnectionTypeGCS, Short: "Add a GCS connection"},
	{Name: "gcp-kms", Type: models.ConnectionTypeGCPKMS, Aliases: []string{"gcpkms"}, Short: "Add a GCP KMS connection"},
	{Name: "facet", Type: models.ConnectionTypeFacet, Short: "Add a Facet connection"},
	{Name: "discord", Type: models.ConnectionTypeDiscord, Short: "Add a Discord connection"},
	{Name: "smtp", Type: models.ConnectionTypeEmail, Aliases: []string{"email"}, Short: "Add an SMTP connection"},
	{Name: "telegram", Type: models.ConnectionTypeTelegram, Short: "Add a Telegram connection"},
	{Name: "ntfy", Type: models.ConnectionTypeNtfy, Short: "Add an Ntfy connection"},
	{Name: "pushbullet", Type: models.ConnectionTypePushbullet, Short: "Add a Pushbullet connection"},
	{Name: "pushover", Type: models.ConnectionTypePushover, Short: "Add a Pushover connection"},
	{Name: "http", Type: models.ConnectionTypeHTTP, Short: "Add an HTTP connection"},
	{Name: "git", Type: models.ConnectionTypeGit, Short: "Add a Git connection"},
	{Name: "github", Type: models.ConnectionTypeGithub, Short: "Add a GitHub connection"},
	{Name: "gitlab", Type: models.ConnectionTypeGitlab, Short: "Add a GitLab connection"},
	{Name: "kubernetes", Type: models.ConnectionTypeKubernetes, Short: "Add a Kubernetes connection"},
	{Name: "folder", Type: models.ConnectionTypeFolder, Short: "Add a folder connection"},
	{Name: "sftp", Type: models.ConnectionTypeSFTP, Short: "Add an SFTP connection"},
	{Name: "smb", Type: models.ConnectionTypeSMB, Short: "Add an SMB connection"},
	{Name: "prometheus", Type: models.ConnectionTypePrometheus, Short: "Add a Prometheus connection"},
	{Name: "loki", Type: models.ConnectionTypeLoki, Short: "Add a Loki connection"},
	{Name: "openai", Type: models.ConnectionTypeOpenAI, Short: "Add an OpenAI connection"},
	{Name: "anthropic", Type: models.ConnectionTypeAnthropic, Short: "Add an Anthropic connection"},
	{Name: "ollama", Type: models.ConnectionTypeOllama, Short: "Add an Ollama connection"},
	{Name: "gemini", Type: models.ConnectionTypeGemini, Short: "Add a Gemini connection"},
}

type ConnectionFlags struct {
	Name      string
	Namespace string
	Type      string
	Test      bool
	DryRun    bool

	// Common fields
	URL         string
	Username    string
	Password    string
	Certificate string
	InsecureTLS bool

	// Kubernetes
	Kubeconfig string

	// AWS
	AccessKey    string
	SecretKey    string
	Region       string
	Profile      string
	FromProfile  string
	SessionToken string

	// AWS KMS / GCP KMS / Azure Key Vault
	KeyID string

	// S3 / GCS
	Bucket       string
	UsePathStyle bool

	// Azure
	ClientID     string
	ClientSecret string
	TenantID     string

	// Azure DevOps
	Organization        string
	PersonalAccessToken string

	// Git
	Ref string

	// Slack
	Channel  string
	Token    string
	BotName  string
	Color    string
	Icon     string
	ThreadTS string
	Title    string

	// SMTP
	Host        string
	Port        int
	FromAddress string
	FromName    string
	ToAddresses []string
	Subject     string
	Auth        string
	Encryption  string

	// Telegram / Ntfy
	Chats string
	Topic string

	// Pushbullet / Pushover
	Targets []string
	User    string

	// Discord
	WebhookID string

	// SFTP / SMB
	Path  string
	Share string

	// Mongo
	ReplicaSet string

	// MSSQL
	TrustServerCertificate bool

	// Database common
	Database string

	// HTTP / Prometheus auth
	Bearer            string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthTokenURL     string
	OAuthScopes       string
	TLSCA             string
	TLSCert           string
	TLSKey            string

	// Facet
	TimestampURL string

	// AI models
	Model  string
	ApiKey string

	// Properties (generic key=value pairs)
	Properties []string
}

func kubeconfigDefault() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

func validateConnectionFlags(flags *ConnectionFlags) error {
	switch flags.Type {
	case models.ConnectionTypeSlack:
		if flags.Token == "" {
			return fmt.Errorf("--token is required for Slack connections")
		}
		if flags.Channel == "" {
			return fmt.Errorf("--channel is required for Slack connections")
		}

	case models.ConnectionTypePostgres, models.ConnectionTypeMySQL, models.ConnectionTypeSQLServer, models.ConnectionTypeMongo:
		if flags.URL == "" && flags.Host == "" {
			return fmt.Errorf("--url or --host is required for database connections")
		}

	case models.ConnectionTypeAWS, models.ConnectionTypeAWSKMS, models.ConnectionTypeS3:
		// AWS can use instance profiles, so credentials are optional

	case models.ConnectionTypeAzure, models.ConnectionTypeAzureKeyVault:
		if flags.ClientID == "" || flags.ClientSecret == "" || flags.TenantID == "" {
			return fmt.Errorf("--client-id, --client-secret, and --tenant-id are required for Azure connections")
		}

	case models.ConnectionTypeAzureDevops:
		if flags.PersonalAccessToken == "" {
			return fmt.Errorf("--personal-access-token is required for Azure DevOps connections")
		}

	case models.ConnectionTypeGCP, models.ConnectionTypeGCS, models.ConnectionTypeGCPKMS:
		// GCP can use default credentials, so certificate is optional

	case models.ConnectionTypeDiscord:
		if flags.WebhookID == "" || flags.Token == "" {
			return fmt.Errorf("--webhook-id and --token are required for Discord connections")
		}

	case models.ConnectionTypeEmail:
		if flags.Host == "" {
			return fmt.Errorf("--host is required for SMTP connections")
		}

	case models.ConnectionTypeTelegram:
		if flags.Token == "" {
			return fmt.Errorf("--token is required for Telegram connections")
		}

	case models.ConnectionTypeNtfy:
		if flags.Host == "" || flags.Topic == "" {
			return fmt.Errorf("--host and --topic are required for Ntfy connections")
		}

	case models.ConnectionTypePushbullet:
		if flags.Token == "" {
			return fmt.Errorf("--token is required for Pushbullet connections")
		}

	case models.ConnectionTypePushover:
		if flags.Token == "" || flags.User == "" {
			return fmt.Errorf("--token and --user are required for Pushover connections")
		}

	case models.ConnectionTypeHTTP:
		if flags.URL == "" {
			return fmt.Errorf("--url is required for HTTP connections")
		}

	case models.ConnectionTypeGit:
		if flags.URL == "" {
			return fmt.Errorf("--url is required for Git connections")
		}

	case models.ConnectionTypeGithub, models.ConnectionTypeGitlab:
		if flags.PersonalAccessToken == "" {
			return fmt.Errorf("--personal-access-token is required for GitHub/GitLab connections")
		}

	case models.ConnectionTypeFolder:
		if flags.Path == "" {
			return fmt.Errorf("--path is required for folder connections")
		}

	case models.ConnectionTypeSFTP:
		if flags.Host == "" {
			return fmt.Errorf("--host is required for SFTP connections")
		}

	case models.ConnectionTypeSMB:
		if flags.Host == "" || flags.Share == "" {
			return fmt.Errorf("--host and --share are required for SMB connections")
		}

	case models.ConnectionTypePrometheus, models.ConnectionTypeLoki:
		if flags.URL == "" {
			return fmt.Errorf("--url is required for %s connections", flags.Type)
		}

	case models.ConnectionTypeOpenAI, models.ConnectionTypeAnthropic, models.ConnectionTypeGemini:
		if flags.ApiKey == "" {
			return fmt.Errorf("--api-key is required for %s connections", flags.Type)
		}

	case models.ConnectionTypeOllama:
		if flags.URL == "" {
			return fmt.Errorf("--url is required for Ollama connections")
		}

	case models.ConnectionTypeFacet:
		if flags.URL == "" {
			return fmt.Errorf("--url is required for Facet connections")
		}
	}

	return nil
}

func runConnectionAdd(flags *ConnectionFlags) error {
	if flags.FromProfile != "" {
		if LocalConnections == nil {
			return fmt.Errorf("--from-profile %w", errNoLocalConnections)
		}
		if err := LocalConnections.LoadAWSProfile(flags); err != nil {
			return err
		}
	}

	if flags.DryRun {
		out, err := marshalDryRunOutput(flags)
		if err != nil {
			return fmt.Errorf("failed to marshal dry-run output: %w", err)
		}
		fmt.Print(string(out))
		return nil
	}

	if err := validateConnectionFlags(flags); err != nil {
		return err
	}

	conn, err := BuildConnectionFromFlags(flags)
	if err != nil {
		return fmt.Errorf("failed to build connection: %w", err)
	}

	if mcCtx, ok := ContextHasAPI(); ok {
		return runConnectionAddViaAPI(mcCtx, flags, &conn)
	}
	if LocalConnections == nil {
		return errNoLocalConnections
	}
	return LocalConnections.AddViaDB(flags, &conn)
}

func runConnectionAddViaAPI(mcCtx *MCContext, flags *ConnectionFlags, conn *models.Connection) error {
	client := NewAPIClient(mcCtx)

	existing, err := client.GetConnection(flags.Name, flags.Namespace)
	if err != nil {
		if !sdk.IsNotFound(err) {
			return fmt.Errorf("failed to check existing connection: %w", err)
		}
	}
	if existing != nil {
		conn.ID = existing.ID
		conn.CreatedAt = existing.CreatedAt
	} else {
		conn.ID = uuid.New()
	}

	if flags.Test {
		if err := client.SaveConnection(conn); err != nil {
			return fmt.Errorf("failed to save connection: %w", err)
		}
		result, err := client.TestConnection(conn.ID.String())
		if err != nil {
			return fmt.Errorf("connection test failed: %w", err)
		}
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		fmt.Println("\nConnection test passed")
		return nil
	}

	if err := client.SaveConnection(conn); err != nil {
		return fmt.Errorf("failed to save connection: %w", err)
	}

	action := "created"
	if existing != nil {
		action = "updated"
	}
	fmt.Printf("Connection '%s' %s in namespace '%s'\n", flags.Name, action, flags.Namespace)
	return nil
}

func newConnectionAddTypeCommand(spec connectionTypeSpec) *cobra.Command {
	flags := &ConnectionFlags{}
	cmd := &cobra.Command{
		Use:               spec.Name,
		Aliases:           spec.Aliases,
		Short:             spec.Short,
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Type = spec.Type
			return runConnectionAdd(flags)
		},
	}
	addConnectionFlags(cmd, flags, spec.Type)

	return cmd
}

func init() {
	for _, spec := range connectionAddTypeSpecs {
		ConnectionAdd.AddCommand(newConnectionAddTypeCommand(spec))
	}

	Connection.AddCommand(ConnectionAdd)
}
