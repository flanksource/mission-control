package cmd

import (
	"errors"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/connection"
)

var Connections = &cobra.Command{
	Use:   "connections",
	Short: "Manage connections",
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

var connFlags connectionFlags

var ConnectionsAdd = &cobra.Command{
	Use:   "add",
	Short: "Add a new connection",
	Long: `Add a new connection to the database.

Examples:
  # Add a Postgres connection
  incident-commander connections add --type=postgres --name=mydb --namespace=default --url="postgres://user:pass@localhost:5432/db"

  # Add an AWS connection
  incident-commander connections add --type=aws --name=my-aws --namespace=default --access-key=AKIA... --secret-key=... --region=us-east-1

  # Add a Slack connection
  incident-commander connections add --type=slack --name=alerts --namespace=default --token=xoxb-xxx --channel=C12345

  # Add and test connection
  incident-commander connections add --type=postgres --name=mydb --namespace=default --url="..." --test`,
	PreRun: PreRun,
	RunE:   runConnectionsAdd,
}

func runConnectionsAdd(cmd *cobra.Command, args []string) error {
	logger.UseSlog()
	if err := properties.LoadFile("mission-control.properties"); err != nil {
		logger.Errorf(err.Error())
	}

	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	if connFlags.Name == "" {
		return fmt.Errorf("--name is required")
	}
	if connFlags.Namespace == "" {
		return fmt.Errorf("--namespace is required")
	}
	if connFlags.Type == "" {
		return fmt.Errorf("--type is required")
	}

	conn, err := buildConnectionFromFlags()
	if err != nil {
		return fmt.Errorf("failed to build connection: %w", err)
	}

	// Check for existing connection
	var existing models.Connection
	err = ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", connFlags.Name, connFlags.Namespace).First(&existing).Error
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

	// Test connection if requested
	if connFlags.Test {
		if err := connection.Test(ctx, &conn); err != nil {
			return fmt.Errorf("connection test failed: %w", err)
		}
		logger.Infof("Connection test passed")
	}

	// Save connection
	if err := ctx.DB().Save(&conn).Error; err != nil {
		return fmt.Errorf("failed to save connection: %w", err)
	}

	if isUpdate {
		fmt.Printf("Connection '%s' updated in namespace '%s'\n", connFlags.Name, connFlags.Namespace)
	} else {
		fmt.Printf("Connection '%s' created in namespace '%s'\n", connFlags.Name, connFlags.Namespace)
	}

	return nil
}

func buildConnectionFromFlags() (models.Connection, error) {
	conn := models.Connection{
		Name:        connFlags.Name,
		Namespace:   connFlags.Namespace,
		Type:        connFlags.Type,
		URL:         connFlags.URL,
		Username:    connFlags.Username,
		Password:    connFlags.Password,
		Certificate: connFlags.Certificate,
		InsecureTLS: connFlags.InsecureTLS,
		Source:      models.SourceUI,
	}

	props := make(map[string]string)

	switch connFlags.Type {
	case models.ConnectionTypeAWS:
		conn.URL = connFlags.URL
		conn.Username = connFlags.AccessKey
		conn.Password = connFlags.SecretKey
		props["region"] = connFlags.Region
		props["profile"] = connFlags.Profile

	case models.ConnectionTypeAWSKMS:
		conn.URL = connFlags.URL
		conn.Username = connFlags.AccessKey
		conn.Password = connFlags.SecretKey
		props["keyID"] = connFlags.KeyID
		props["region"] = connFlags.Region
		props["profile"] = connFlags.Profile

	case models.ConnectionTypeS3:
		conn.URL = connFlags.URL
		conn.Username = connFlags.AccessKey
		conn.Password = connFlags.SecretKey
		props["bucket"] = connFlags.Bucket
		props["region"] = connFlags.Region
		props["profile"] = connFlags.Profile
		props["usePathStyle"] = fmt.Sprintf("%t", connFlags.UsePathStyle)

	case models.ConnectionTypeAzure:
		conn.Username = connFlags.ClientID
		conn.Password = connFlags.ClientSecret
		props["tenant"] = connFlags.TenantID

	case models.ConnectionTypeAzureKeyVault:
		conn.Username = connFlags.ClientID
		conn.Password = connFlags.ClientSecret
		props["tenant"] = connFlags.TenantID
		props["keyID"] = connFlags.KeyID

	case models.ConnectionTypeAzureDevops:
		conn.URL = connFlags.URL
		conn.Username = connFlags.Organization
		conn.Password = connFlags.PersonalAccessToken

	case models.ConnectionTypeGCP:
		conn.URL = connFlags.URL
		conn.Certificate = connFlags.Certificate

	case models.ConnectionTypeGCS:
		conn.URL = connFlags.URL
		conn.Certificate = connFlags.Certificate
		props["bucket"] = connFlags.Bucket

	case models.ConnectionTypeGCPKMS:
		conn.URL = connFlags.URL
		conn.Certificate = connFlags.Certificate
		props["keyID"] = connFlags.KeyID

	case models.ConnectionTypePostgres:
		if connFlags.URL == "" && connFlags.Host != "" {
			conn.URL = fmt.Sprintf("postgres://$(username):$(password)@%s/%s", connFlags.Host, connFlags.Database)
			if connFlags.InsecureTLS {
				conn.URL += "?sslmode=disable"
			}
		}
		props["host"] = connFlags.Host
		props["database"] = connFlags.Database

	case models.ConnectionTypeMySQL:
		if connFlags.URL == "" && connFlags.Host != "" {
			conn.URL = fmt.Sprintf("mysql://$(username):$(password)@%s/%s", connFlags.Host, connFlags.Database)
		}
		props["host"] = connFlags.Host
		props["database"] = connFlags.Database

	case models.ConnectionTypeSQLServer, "mssql":
		conn.Type = models.ConnectionTypeSQLServer
		if connFlags.URL == "" && connFlags.Host != "" {
			conn.URL = fmt.Sprintf("sqlserver://$(username):$(password)@%s?database=%s", connFlags.Host, connFlags.Database)
			if connFlags.TrustServerCertificate {
				conn.URL += "&TrustServerCertificate=true"
			}
		}
		props["host"] = connFlags.Host
		props["database"] = connFlags.Database

	case models.ConnectionTypeMongo:
		if connFlags.URL == "" && connFlags.Host != "" {
			conn.URL = fmt.Sprintf("mongodb://$(username):$(password)@%s/%s", connFlags.Host, connFlags.Database)
			if connFlags.ReplicaSet != "" {
				conn.URL += "?replicaSet=" + connFlags.ReplicaSet
			}
		}
		props["host"] = connFlags.Host
		props["database"] = connFlags.Database
		props["replica_set"] = connFlags.ReplicaSet

	case models.ConnectionTypeSlack:
		conn.URL = "slack://$(password)@$(username)"
		conn.Username = connFlags.Channel
		conn.Password = connFlags.Token
		props["BotName"] = connFlags.BotName
		props["Color"] = connFlags.Color
		props["Icon"] = connFlags.Icon
		props["ThreadTS"] = connFlags.ThreadTS
		props["Title"] = connFlags.Title

	case models.ConnectionTypeDiscord:
		conn.URL = "discord://$(password)@$(username)"
		conn.Username = connFlags.WebhookID
		conn.Password = connFlags.Token

	case models.ConnectionTypeEmail:
		port := connFlags.Port
		if port == 0 {
			port = 587
		}
		conn.URL = fmt.Sprintf("smtp://$(username):$(password)@%s:%d/", connFlags.Host, port)
		props["port"] = fmt.Sprintf("%d", port)
		props["from"] = connFlags.FromAddress
		props["fromname"] = connFlags.FromName
		props["subject"] = connFlags.Subject
		props["auth"] = connFlags.Auth

	case models.ConnectionTypeTelegram:
		conn.URL = "telegram://$(password)@telegram/?Chats=$(username)"
		conn.Username = connFlags.Chats
		conn.Password = connFlags.Token

	case models.ConnectionTypeNtfy:
		conn.URL = fmt.Sprintf("ntfy://$(username):$(password)@%s/%s", connFlags.Host, connFlags.Topic)
		props["topic"] = connFlags.Topic

	case models.ConnectionTypePushbullet:
		conn.URL = "pushbullet://$(password)/"
		conn.Password = connFlags.Token

	case models.ConnectionTypePushover:
		conn.URL = "pushover://:$(password)@$(username)"
		conn.Username = connFlags.User
		conn.Password = connFlags.Token

	case models.ConnectionTypeHTTP:
		props["insecure_tls"] = fmt.Sprintf("%t", connFlags.InsecureTLS)

	case models.ConnectionTypeGit:
		props["ref"] = connFlags.Ref

	case models.ConnectionTypeGithub:
		conn.Password = connFlags.PersonalAccessToken

	case models.ConnectionTypeGitlab:
		conn.Password = connFlags.PersonalAccessToken

	case models.ConnectionTypeKubernetes:
		conn.Certificate = connFlags.Certificate

	case models.ConnectionTypeFolder:
		props["path"] = connFlags.Path

	case models.ConnectionTypeSFTP:
		port := connFlags.Port
		if port == 0 {
			port = 22
		}
		props["path"] = connFlags.Path
		props["port"] = fmt.Sprintf("%d", port)

	case models.ConnectionTypeSMB:
		props["port"] = fmt.Sprintf("%d", connFlags.Port)
		props["share"] = connFlags.Share

	case models.ConnectionTypePrometheus:
		props["bearer"] = connFlags.Bearer

	case models.ConnectionTypeLoki:
		// URL, username, password already set

	case models.ConnectionTypeOpenAI, models.ConnectionTypeAnthropic, models.ConnectionTypeOllama, models.ConnectionTypeGemini:
		conn.Password = connFlags.ApiKey
		if connFlags.Model != "" {
			props["model"] = connFlags.Model
		}

	default:
		return conn, fmt.Errorf("unsupported connection type: %s", connFlags.Type)
	}

	// Filter out empty properties
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

func init() {
	// Common flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Name, "name", "", "Connection name (required)")
	ConnectionsAdd.Flags().StringVar(&connFlags.Namespace, "namespace", "", "Connection namespace (required)")
	ConnectionsAdd.Flags().StringVar(&connFlags.Type, "type", "", "Connection type (required): postgres, mysql, mssql, mongo, aws, azure, gcp, s3, gcs, slack, smtp, http, git, github, gitlab, etc.")
	ConnectionsAdd.Flags().BoolVar(&connFlags.Test, "test", false, "Test connection before saving")

	// Common connection fields
	ConnectionsAdd.Flags().StringVar(&connFlags.URL, "url", "", "Connection URL")
	ConnectionsAdd.Flags().StringVar(&connFlags.Username, "username", "", "Username")
	ConnectionsAdd.Flags().StringVar(&connFlags.Password, "password", "", "Password")
	ConnectionsAdd.Flags().StringVar(&connFlags.Certificate, "certificate", "", "Certificate/credentials")
	ConnectionsAdd.Flags().BoolVar(&connFlags.InsecureTLS, "insecure-tls", false, "Skip TLS verification")

	// AWS flags
	ConnectionsAdd.Flags().StringVar(&connFlags.AccessKey, "access-key", "", "AWS access key")
	ConnectionsAdd.Flags().StringVar(&connFlags.SecretKey, "secret-key", "", "AWS secret key")
	ConnectionsAdd.Flags().StringVar(&connFlags.Region, "region", "", "AWS/cloud region")
	ConnectionsAdd.Flags().StringVar(&connFlags.Profile, "profile", "", "AWS profile")

	// KMS flags
	ConnectionsAdd.Flags().StringVar(&connFlags.KeyID, "key-id", "", "KMS key ID")

	// S3/GCS flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Bucket, "bucket", "", "S3/GCS bucket name")
	ConnectionsAdd.Flags().BoolVar(&connFlags.UsePathStyle, "use-path-style", false, "Use path-style S3 URLs")

	// Azure flags
	ConnectionsAdd.Flags().StringVar(&connFlags.ClientID, "client-id", "", "Azure client ID")
	ConnectionsAdd.Flags().StringVar(&connFlags.ClientSecret, "client-secret", "", "Azure client secret")
	ConnectionsAdd.Flags().StringVar(&connFlags.TenantID, "tenant-id", "", "Azure tenant ID")

	// Azure DevOps / GitHub / GitLab flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Organization, "organization", "", "Azure DevOps organization")
	ConnectionsAdd.Flags().StringVar(&connFlags.PersonalAccessToken, "personal-access-token", "", "Personal access token")

	// Git flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Ref, "ref", "", "Git reference (branch/tag)")

	// Slack flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Channel, "channel", "", "Slack channel ID")
	ConnectionsAdd.Flags().StringVar(&connFlags.Token, "token", "", "API token")
	ConnectionsAdd.Flags().StringVar(&connFlags.BotName, "bot-name", "", "Slack bot name")
	ConnectionsAdd.Flags().StringVar(&connFlags.Color, "color", "", "Slack message color")
	ConnectionsAdd.Flags().StringVar(&connFlags.Icon, "icon", "", "Slack icon")
	ConnectionsAdd.Flags().StringVar(&connFlags.ThreadTS, "thread-ts", "", "Slack thread timestamp")
	ConnectionsAdd.Flags().StringVar(&connFlags.Title, "title", "", "Message title")

	// SMTP flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Host, "host", "", "Host address")
	ConnectionsAdd.Flags().IntVar(&connFlags.Port, "port", 0, "Port number")
	ConnectionsAdd.Flags().StringVar(&connFlags.FromAddress, "from-address", "", "From email address")
	ConnectionsAdd.Flags().StringVar(&connFlags.FromName, "from-name", "", "From name")
	ConnectionsAdd.Flags().StringSliceVar(&connFlags.ToAddresses, "to-addresses", nil, "To email addresses")
	ConnectionsAdd.Flags().StringVar(&connFlags.Subject, "subject", "", "Email subject")
	ConnectionsAdd.Flags().StringVar(&connFlags.Auth, "auth", "", "SMTP auth method")
	ConnectionsAdd.Flags().StringVar(&connFlags.Encryption, "encryption", "", "SMTP encryption")

	// Telegram/Ntfy flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Chats, "chats", "", "Telegram chat IDs")
	ConnectionsAdd.Flags().StringVar(&connFlags.Topic, "topic", "", "Ntfy topic")

	// Pushbullet/Pushover flags
	ConnectionsAdd.Flags().StringSliceVar(&connFlags.Targets, "targets", nil, "Pushbullet targets")
	ConnectionsAdd.Flags().StringVar(&connFlags.User, "user", "", "Pushover user key")

	// Discord flags
	ConnectionsAdd.Flags().StringVar(&connFlags.WebhookID, "webhook-id", "", "Discord webhook ID")

	// SFTP/SMB flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Path, "path", "", "File path")
	ConnectionsAdd.Flags().StringVar(&connFlags.Share, "share", "", "SMB share name")

	// Database flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Database, "database", "", "Database name")
	ConnectionsAdd.Flags().StringVar(&connFlags.ReplicaSet, "replica-set", "", "MongoDB replica set")
	ConnectionsAdd.Flags().BoolVar(&connFlags.TrustServerCertificate, "trust-server-certificate", false, "MSSQL trust server certificate")

	// Prometheus flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Bearer, "bearer", "", "Bearer token")

	// AI flags
	ConnectionsAdd.Flags().StringVar(&connFlags.Model, "model", "", "AI model name")
	ConnectionsAdd.Flags().StringVar(&connFlags.ApiKey, "api-key", "", "API key")

	Connections.AddCommand(ConnectionsAdd)
	Root.AddCommand(Connections)
}
