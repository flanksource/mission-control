package clientapi

import (
	"time"

	"github.com/google/uuid"
)

const (
	ConnectionTypeAnthropic     = "anthropic"
	ConnectionTypeAWS           = "aws"
	ConnectionTypeAWSKMS        = "aws_kms"
	ConnectionTypeAzure         = "azure"
	ConnectionTypeAzureDevops   = "azure_devops"
	ConnectionTypeAzureKeyVault = "azure_key_vault"
	ConnectionTypeDiscord       = "discord"
	ConnectionTypeElasticSearch = "elasticsearch"
	ConnectionTypeEmail         = "email"
	ConnectionTypeFacet         = "facet"
	ConnectionTypeFolder        = "folder"
	ConnectionTypeGCP           = "google_cloud"
	ConnectionTypeGCPKMS        = "gcp_kms"
	ConnectionTypeGCS           = "gcs"
	ConnectionTypeGemini        = "gemini"
	ConnectionTypeGit           = "git"
	ConnectionTypeGithub        = "github"
	ConnectionTypeGitlab        = "gitlab"
	ConnectionTypeHTTP          = "http"
	ConnectionTypeKubernetes    = "kubernetes"
	ConnectionTypeLoki          = "loki"
	ConnectionTypeMongo         = "mongo"
	ConnectionTypeMySQL         = "mysql"
	ConnectionTypeNtfy          = "ntfy"
	ConnectionTypeOllama        = "ollama"
	ConnectionTypeOpenAI        = "openai"
	ConnectionTypePostgres      = "postgres"
	ConnectionTypePrometheus    = "prometheus"
	ConnectionTypePushbullet    = "pushbullet"
	ConnectionTypePushover      = "pushover"
	ConnectionTypeRedis         = "redis"
	ConnectionTypeS3            = "s3"
	ConnectionTypeSFTP          = "sftp"
	ConnectionTypeSlack         = "slack"
	ConnectionTypeSMB           = "smb"
	ConnectionTypeSQLServer     = "sql_server"
	ConnectionTypeTelegram      = "telegram"
)

type Connection struct {
	ID          uuid.UUID         `json:"id"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Source      string            `json:"source"`
	Type        string            `json:"type"`
	URL         string            `json:"url,omitempty"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Certificate string            `json:"certificate,omitempty"`
	InsecureTLS bool              `json:"insecure_tls,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty,omitzero"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty,omitzero"`
	CreatedBy   *uuid.UUID        `json:"created_by,omitempty"`
}

type EnvVar struct {
	Name        string        `json:"name,omitempty" yaml:"name,omitempty"`
	ValueStatic string        `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom   *EnvVarSource `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}

type EnvVarSource struct {
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty" yaml:"secretKeyRef,omitempty"`
}

type SecretKeySelector struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Key  string `json:"key" yaml:"key"`
}
