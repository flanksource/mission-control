package cmd

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/connection"
)

var Connection = &cobra.Command{
	Use:   "connection",
	Short: "Manage connections",
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
	PersistentPreRun:  PreRun,
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

type connectionFlags struct {
	Name      string
	Namespace string
	Type      string
	Test      bool

	// Common fields
	URL         string
	Username    string
	Password    string
	Certificate string
	InsecureTLS bool

	// AWS
	AccessKey string
	SecretKey string
	Region    string
	Profile   string

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

	// Prometheus
	Bearer string

	// AI models
	Model  string
	ApiKey string

	// Properties (generic key=value pairs)
	Properties []string
}

func runConnectionAdd(flags *connectionFlags) error {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	if flags.Name == "" {
		return fmt.Errorf("--name is required")
	}
	if flags.Namespace == "" {
		return fmt.Errorf("--namespace is required")
	}
	conn, err := buildConnectionFromFlags(flags)
	if err != nil {
		return fmt.Errorf("failed to build connection: %w", err)
	}

	var existing models.Connection
	err = ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", flags.Name, flags.Namespace).First(&existing).Error
	isUpdate := false
	if err == nil {
		isUpdate = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing connection: %w", err)
	}

	if isUpdate {
		conn.ID = existing.ID
		conn.CreatedAt = existing.CreatedAt
	} else {
		conn.ID = uuid.New()
	}

	if flags.Test {
		if err := connection.Test(ctx, &conn); err != nil {
			return fmt.Errorf("connection test failed: %w", err)
		}
		logger.Infof("Connection test passed")
	}

	if err := ctx.DB().Save(&conn).Error; err != nil {
		return fmt.Errorf("failed to save connection: %w", err)
	}

	if isUpdate {
		fmt.Printf("Connection '%s' updated in namespace '%s'\n", flags.Name, flags.Namespace)
	} else {
		fmt.Printf("Connection '%s' created in namespace '%s'\n", flags.Name, flags.Namespace)
	}

	return nil
}

func buildConnectionFromFlags(flags *connectionFlags) (models.Connection, error) {
	conn := models.Connection{
		Name:        flags.Name,
		Namespace:   flags.Namespace,
		Type:        flags.Type,
		URL:         flags.URL,
		Username:    flags.Username,
		Password:    flags.Password,
		Certificate: flags.Certificate,
		InsecureTLS: flags.InsecureTLS,
		Source:      models.SourceUI,
	}

	props := make(map[string]string)

	switch flags.Type {
	case models.ConnectionTypeAWS:
		conn.URL = flags.URL
		conn.Username = flags.AccessKey
		conn.Password = flags.SecretKey
		props["region"] = flags.Region
		props["profile"] = flags.Profile

	case models.ConnectionTypeAWSKMS:
		conn.URL = flags.URL
		conn.Username = flags.AccessKey
		conn.Password = flags.SecretKey
		props["keyID"] = flags.KeyID
		props["region"] = flags.Region
		props["profile"] = flags.Profile

	case models.ConnectionTypeS3:
		conn.URL = flags.URL
		conn.Username = flags.AccessKey
		conn.Password = flags.SecretKey
		props["bucket"] = flags.Bucket
		props["region"] = flags.Region
		props["profile"] = flags.Profile
		props["usePathStyle"] = fmt.Sprintf("%t", flags.UsePathStyle)

	case models.ConnectionTypeAzure:
		conn.Username = flags.ClientID
		conn.Password = flags.ClientSecret
		props["tenant"] = flags.TenantID

	case models.ConnectionTypeAzureKeyVault:
		conn.Username = flags.ClientID
		conn.Password = flags.ClientSecret
		props["tenant"] = flags.TenantID
		props["keyID"] = flags.KeyID

	case models.ConnectionTypeAzureDevops:
		conn.URL = flags.URL
		conn.Username = flags.Organization
		conn.Password = flags.PersonalAccessToken

	case models.ConnectionTypeGCP:
		conn.URL = flags.URL
		conn.Certificate = flags.Certificate

	case models.ConnectionTypeGCS:
		conn.URL = flags.URL
		conn.Certificate = flags.Certificate
		props["bucket"] = flags.Bucket

	case models.ConnectionTypeGCPKMS:
		conn.URL = flags.URL
		conn.Certificate = flags.Certificate
		props["keyID"] = flags.KeyID

	case models.ConnectionTypePostgres:
		if flags.URL == "" && flags.Host != "" {
			conn.URL = fmt.Sprintf("postgres://$(username):$(password)@%s/%s", flags.Host, flags.Database)
			if flags.InsecureTLS {
				conn.URL += "?sslmode=disable"
			}
		}
		props["host"] = flags.Host
		props["database"] = flags.Database

	case models.ConnectionTypeMySQL:
		if flags.URL == "" && flags.Host != "" {
			conn.URL = fmt.Sprintf("mysql://$(username):$(password)@%s/%s", flags.Host, flags.Database)
		}
		props["host"] = flags.Host
		props["database"] = flags.Database

	case models.ConnectionTypeSQLServer:
		if flags.URL == "" && flags.Host != "" {
			conn.URL = fmt.Sprintf("sqlserver://$(username):$(password)@%s?database=%s", flags.Host, flags.Database)
			if flags.TrustServerCertificate {
				conn.URL += "&TrustServerCertificate=true"
			}
		}
		props["host"] = flags.Host
		props["database"] = flags.Database

	case models.ConnectionTypeMongo:
		if flags.URL == "" && flags.Host != "" {
			conn.URL = fmt.Sprintf("mongodb://$(username):$(password)@%s/%s", flags.Host, flags.Database)
			if flags.ReplicaSet != "" {
				conn.URL += "?replicaSet=" + flags.ReplicaSet
			}
		}
		props["host"] = flags.Host
		props["database"] = flags.Database
		props["replica_set"] = flags.ReplicaSet

	case models.ConnectionTypeSlack:
		conn.URL = "slack://$(password)@$(username)"
		conn.Username = flags.Channel
		conn.Password = flags.Token
		props["BotName"] = flags.BotName
		props["Color"] = flags.Color
		props["Icon"] = flags.Icon
		props["ThreadTS"] = flags.ThreadTS
		props["Title"] = flags.Title

	case models.ConnectionTypeDiscord:
		conn.URL = "discord://$(password)@$(username)"
		conn.Username = flags.WebhookID
		conn.Password = flags.Token

	case models.ConnectionTypeEmail:
		port := flags.Port
		if port == 0 {
			port = 587
		}
		conn.URL = fmt.Sprintf("smtp://$(username):$(password)@%s:%d/", flags.Host, port)
		props["port"] = fmt.Sprintf("%d", port)
		props["from"] = flags.FromAddress
		props["fromname"] = flags.FromName
		props["subject"] = flags.Subject
		props["auth"] = flags.Auth

	case models.ConnectionTypeTelegram:
		conn.URL = "telegram://$(password)@telegram/?Chats=$(username)"
		conn.Username = flags.Chats
		conn.Password = flags.Token

	case models.ConnectionTypeNtfy:
		conn.URL = fmt.Sprintf("ntfy://$(username):$(password)@%s/%s", flags.Host, flags.Topic)
		props["topic"] = flags.Topic

	case models.ConnectionTypePushbullet:
		conn.URL = "pushbullet://$(password)/"
		conn.Password = flags.Token

	case models.ConnectionTypePushover:
		conn.URL = "pushover://:$(password)@$(username)"
		conn.Username = flags.User
		conn.Password = flags.Token

	case models.ConnectionTypeHTTP:
		props["insecure_tls"] = fmt.Sprintf("%t", flags.InsecureTLS)

	case models.ConnectionTypeGit:
		props["ref"] = flags.Ref

	case models.ConnectionTypeGithub:
		conn.Password = flags.PersonalAccessToken

	case models.ConnectionTypeGitlab:
		conn.Password = flags.PersonalAccessToken

	case models.ConnectionTypeKubernetes:
		conn.Certificate = flags.Certificate

	case models.ConnectionTypeFolder:
		props["path"] = flags.Path

	case models.ConnectionTypeSFTP:
		port := flags.Port
		if port == 0 {
			port = 22
		}
		props["path"] = flags.Path
		props["port"] = fmt.Sprintf("%d", port)

	case models.ConnectionTypeSMB:
		props["port"] = fmt.Sprintf("%d", flags.Port)
		props["share"] = flags.Share

	case models.ConnectionTypePrometheus:
		props["bearer"] = flags.Bearer

	case models.ConnectionTypeLoki:
		// URL, username, password already set

	case models.ConnectionTypeOpenAI, models.ConnectionTypeAnthropic, models.ConnectionTypeOllama, models.ConnectionTypeGemini:
		conn.Password = flags.ApiKey
		if flags.Model != "" {
			props["model"] = flags.Model
		}

	default:
		return conn, fmt.Errorf("unsupported connection type: %s", flags.Type)
	}

	filteredProps := make(map[string]string)
	for k, v := range props {
		if v != "" && v != "false" && v != "0" {
			filteredProps[k] = v
		}
	}
	if len(filteredProps) > 0 {
		conn.Properties = filteredProps
	}

	return conn, nil
}

func addConnectionFlags(cmd *cobra.Command, flags *connectionFlags) {
	cmd.Flags().StringVar(&flags.Name, "name", "", "Connection name (required)")
	cmd.Flags().StringVar(&flags.Namespace, "namespace", "", "Connection namespace (required)")
	cmd.Flags().BoolVar(&flags.Test, "test", false, "Test connection before saving")

	cmd.Flags().StringVar(&flags.URL, "url", "", "Connection URL")
	cmd.Flags().StringVar(&flags.Username, "username", "", "Username")
	cmd.Flags().StringVar(&flags.Password, "password", "", "Password")
	cmd.Flags().StringVar(&flags.Certificate, "certificate", "", "Certificate/credentials")
	cmd.Flags().BoolVar(&flags.InsecureTLS, "insecure-tls", false, "Skip TLS verification")

	cmd.Flags().StringVar(&flags.AccessKey, "access-key", "", "AWS access key")
	cmd.Flags().StringVar(&flags.SecretKey, "secret-key", "", "AWS secret key")
	cmd.Flags().StringVar(&flags.Region, "region", "", "AWS/cloud region")
	cmd.Flags().StringVar(&flags.Profile, "profile", "", "AWS profile")

	cmd.Flags().StringVar(&flags.KeyID, "key-id", "", "KMS key ID")

	cmd.Flags().StringVar(&flags.Bucket, "bucket", "", "S3/GCS bucket name")
	cmd.Flags().BoolVar(&flags.UsePathStyle, "use-path-style", false, "Use path-style S3 URLs")

	cmd.Flags().StringVar(&flags.ClientID, "client-id", "", "Azure client ID")
	cmd.Flags().StringVar(&flags.ClientSecret, "client-secret", "", "Azure client secret")
	cmd.Flags().StringVar(&flags.TenantID, "tenant-id", "", "Azure tenant ID")

	cmd.Flags().StringVar(&flags.Organization, "organization", "", "Azure DevOps organization")
	cmd.Flags().StringVar(&flags.PersonalAccessToken, "personal-access-token", "", "Personal access token")

	cmd.Flags().StringVar(&flags.Ref, "ref", "", "Git reference (branch/tag)")

	cmd.Flags().StringVar(&flags.Channel, "channel", "", "Slack channel ID")
	cmd.Flags().StringVar(&flags.Token, "token", "", "API token")
	cmd.Flags().StringVar(&flags.BotName, "bot-name", "", "Slack bot name")
	cmd.Flags().StringVar(&flags.Color, "color", "", "Slack message color")
	cmd.Flags().StringVar(&flags.Icon, "icon", "", "Slack icon")
	cmd.Flags().StringVar(&flags.ThreadTS, "thread-ts", "", "Slack thread timestamp")
	cmd.Flags().StringVar(&flags.Title, "title", "", "Message title")

	cmd.Flags().StringVar(&flags.Host, "host", "", "Host address")
	cmd.Flags().IntVar(&flags.Port, "port", 0, "Port number")
	cmd.Flags().StringVar(&flags.FromAddress, "from-address", "", "From email address")
	cmd.Flags().StringVar(&flags.FromName, "from-name", "", "From name")
	cmd.Flags().StringSliceVar(&flags.ToAddresses, "to-addresses", nil, "To email addresses")
	cmd.Flags().StringVar(&flags.Subject, "subject", "", "Email subject")
	cmd.Flags().StringVar(&flags.Auth, "auth", "", "SMTP auth method")
	cmd.Flags().StringVar(&flags.Encryption, "encryption", "", "SMTP encryption")

	cmd.Flags().StringVar(&flags.Chats, "chats", "", "Telegram chat IDs")
	cmd.Flags().StringVar(&flags.Topic, "topic", "", "Ntfy topic")

	cmd.Flags().StringSliceVar(&flags.Targets, "targets", nil, "Pushbullet targets")
	cmd.Flags().StringVar(&flags.User, "user", "", "Pushover user key")

	cmd.Flags().StringVar(&flags.WebhookID, "webhook-id", "", "Discord webhook ID")

	cmd.Flags().StringVar(&flags.Path, "path", "", "File path")
	cmd.Flags().StringVar(&flags.Share, "share", "", "SMB share name")

	cmd.Flags().StringVar(&flags.Database, "database", "", "Database name")
	cmd.Flags().StringVar(&flags.ReplicaSet, "replica-set", "", "MongoDB replica set")
	cmd.Flags().BoolVar(&flags.TrustServerCertificate, "trust-server-certificate", false, "MSSQL trust server certificate")

	cmd.Flags().StringVar(&flags.Bearer, "bearer", "", "Bearer token")

	cmd.Flags().StringVar(&flags.Model, "model", "", "AI model name")
	cmd.Flags().StringVar(&flags.ApiKey, "api-key", "", "API key")
}

func newConnectionAddTypeCommand(spec connectionTypeSpec) *cobra.Command {
	flags := &connectionFlags{}
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
	addConnectionFlags(cmd, flags)

	return cmd
}

func init() {
	for _, spec := range connectionAddTypeSpecs {
		ConnectionAdd.AddCommand(newConnectionAddTypeCommand(spec))
	}

	Connection.AddCommand(ConnectionAdd)
	Root.AddCommand(Connection)
}
