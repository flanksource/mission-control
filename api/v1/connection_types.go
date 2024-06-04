package v1

import (
	"github.com/flanksource/duty/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConnectionTelegram struct {
	Token types.EnvVar `json:"token"`

	// Chat ID or Channel name (using @channel-name)
	Chats types.EnvVar `json:"chats"`
}

type ConnectionSlack struct {
	Token types.EnvVar `json:"token"`

	// Channel to send messages to in Cxxxxxxxxxx format
	Channel string `json:"channel"`

	BotName string `json:"botname"`
}

type ConnectionNtfy struct {
	Topic string `json:"topic"`

	Host     string       `json:"host"`
	Username types.EnvVar `json:"username"`
	Password types.EnvVar `json:"password"`
}

type ConnectionDiscord struct {
	Token     string `json:"token"`
	WebhookID string `json:"webhookID"`
}

type ConnectionSMTP struct {
	Host     string       `json:"host"`
	Username types.EnvVar `json:"username,omitempty"`
	Password types.EnvVar `json:"password,omitempty"`

	// Default: false
	InsecureTLS bool `json:"insecureTLS,omitempty"`

	// Encryption Method
	// 	Defulat: auto
	// 	Possible values: None, ExplicitTLS, ImplicitTLS, Auto
	Encryption string `json:"encryption,omitempty"`

	// SMTP server port
	// 	Default: 587
	Port int `json:"port,omitempty"`

	// Email address that the mail are sent from
	FromAddress string `json:"fromAddress"`

	// List of recipient e-mails
	ToAddresses []string `json:"toAddresses,omitempty"`

	// The subject of the sent mail
	Subject string `json:"subject,omitempty"`

	// Auth - SMTP authentication method
	// Possible values: None, Plain, CRAMMD5, Unknown, OAuth2
	Auth string `json:"auth,omitempty"`
}

type ConnectionPushbullet struct {
	Token types.EnvVar `json:"token"`

	Targets []string `json:"targets"`
}

type ConnectionPushover struct {
	Token types.EnvVar `json:"token"`
	// User key
	User string `json:"user"`
}

type ConnectionPostgres struct {
	// URL is the connection url.
	URL types.EnvVar `json:"url,omitempty"`

	// <host:port>
	Host        types.EnvVar `json:"host,omitempty"`
	Username    types.EnvVar `json:"username,omitempty"`
	Password    types.EnvVar `json:"password,omitempty"`
	Database    types.EnvVar `json:"database,omitempty"`
	InsecureTLS bool         `json:"insecureTLS,omitempty"`
}

type ConnectionMySQL struct {
	// URL is the connection url.
	URL types.EnvVar `json:"url,omitempty"`

	// <host:port>
	Host        types.EnvVar `json:"host,omitempty"`
	Username    types.EnvVar `json:"username,omitempty"`
	Password    types.EnvVar `json:"password,omitempty"`
	Database    types.EnvVar `json:"database,omitempty"`
	InsecureTLS bool         `json:"insecureTLS,omitempty"`
}

type ConnectionMSSQL struct {
	// URL is the connection url.
	URL types.EnvVar `json:"url,omitempty"`

	// <host:port>
	Host                   types.EnvVar `json:"host,omitempty"`
	Username               types.EnvVar `json:"username,omitempty"`
	Password               types.EnvVar `json:"password,omitempty"`
	Database               types.EnvVar `json:"database,omitempty"`
	TrustServerCertificate bool         `json:"trustServerCertificate,omitempty"`
}

type ConnectionMongo struct {
	// URL is the connection url.
	URL types.EnvVar `json:"url,omitempty"`

	// <host:port>
	Host        types.EnvVar `json:"host,omitempty"`
	Username    types.EnvVar `json:"username,omitempty"`
	Password    types.EnvVar `json:"password,omitempty"`
	Database    types.EnvVar `json:"database,omitempty"`
	ReplicaSet  string       `json:"replicaSet,omitempty"`
	InsecureTLS bool         `json:"insecureTLS,omitempty"`
}

type ConnectionAWSS3 struct {
	ConnectionAWS `json:",inline"`
	Bucket        string `json:"bucket"`
}

type ConnectionAWS struct {
	Region      string       `json:"region,omitempty"`
	Profile     string       `json:"profile,omitempty"`
	InsecureTLS bool         `json:"insecureTLS,omitempty"`
	AccessKey   types.EnvVar `json:"accessKey,omitempty"`
	SecretKey   types.EnvVar `json:"secretKey,omitempty"`
}

type ConnectionAzure struct {
	ClientID     types.EnvVar `json:"clientID"`
	ClientSecret types.EnvVar `json:"clientSecret,omitempty"`
	TenantID     types.EnvVar `json:"tenantID"`
}

type ConnectionAzureDevops struct {
	URL                 string       `json:"string,omitempty"`
	Organization        string       `json:"organization"`
	PersonalAccessToken types.EnvVar `json:"personalAccessToken"`
}

type ConnectionGCP struct {
	Endpoint    types.EnvVar `json:"endpoint,omitempty"`
	Certificate types.EnvVar `json:"certificate,omitempty"`
}

type ConnectionFolder struct {
	Path string `json:"path"`
}

type ConnectionKubernetes struct {
	Certificate types.EnvVar `json:"certificate"`
}

type ConnectionGitHub struct {
	URL                 string       `json:"url,omitempty"`
	PersonalAccessToken types.EnvVar `json:"personalAccessToken"`
}

type ConnectionGitLab struct {
	URL                 string       `json:"string,omitempty"`
	PersonalAccessToken types.EnvVar `json:"personalAccessToken"`
}

type ConnectionGit struct {
	URL         string        `json:"string"`
	Ref         string        `json:"ref"`
	Certificate *types.EnvVar `json:"certificate,omitempty"`
	Username    *types.EnvVar `json:"username,omitempty"`
	Password    *types.EnvVar `json:"password,omitempty"`
}

type ConnectionHTTP struct {
	URL         string        `json:"url"`
	InsecureTLS bool          `json:"insecureTLS,omitempty"`
	Username    *types.EnvVar `json:"username,omitempty"`
	Password    *types.EnvVar `json:"password,omitempty"`
}

type ConnectionSFTP struct {
	Host     types.EnvVar `json:"host"`
	Username types.EnvVar `json:"username"`
	Password types.EnvVar `json:"password"`

	// SMTP server port
	// 	Default: 22
	Port int `json:"port,omitempty"`

	Path string `json:"path"`
}

type ConnectionSMB struct {
	Server   types.EnvVar `json:"server"`
	Username types.EnvVar `json:"username"`
	Password types.EnvVar `json:"password"`

	// SMB server port
	// 	Default: 445
	Port types.EnvVar `json:"port,omitempty"`

	Share string `json:"share"`
}

// ConnectionSpec defines the desired state of Connection
type ConnectionSpec struct {
	Properties types.JSONStringMap `json:"properties,omitempty"`

	AWS         *ConnectionAWS         `json:"aws,omitempty"`
	S3          *ConnectionAWSS3       `json:"s3,omitempty"`
	Azure       *ConnectionAzure       `json:"azure,omitempty"`
	AzureDevops *ConnectionAzureDevops `json:"azureDevops,omitempty"`
	GCP         *ConnectionGCP         `json:"gcp,omitempty"`

	Folder     *ConnectionFolder     `json:"folder,omitempty"`
	Git        *ConnectionGit        `json:"git,omitempty"`
	GitHub     *ConnectionGitHub     `json:"github,omitempty"`
	GitLab     *ConnectionGitLab     `json:"gitlab,omitempty"`
	HTTP       *ConnectionHTTP       `json:"http,omitempty"`
	Kubernetes *ConnectionKubernetes `json:"kubernetes,omitempty"`
	MSSQL      *ConnectionMSSQL      `json:"mssql,omitempty"`
	Mongo      *ConnectionMongo      `json:"mongo,omitempty"`
	MySQL      *ConnectionMySQL      `json:"mysql,omitempty"`
	Postgres   *ConnectionPostgres   `json:"postgres,omitempty"`
	SFTP       *ConnectionSFTP       `json:"sftp,omitempty"`
	SMB        *ConnectionSMB        `json:"smb,omitempty"`

	//////////////////////////////
	// Notification Connections //
	//////////////////////////////

	Discord    *ConnectionDiscord    `json:"discord,omitempty"`
	Ntfy       *ConnectionNtfy       `json:"ntfy,omitempty"`
	Pushbullet *ConnectionPushbullet `json:"pushbullet,omitempty"`
	Pushover   *ConnectionPushover   `json:"pushover,omitempty"`
	SMTP       *ConnectionSMTP       `json:"smtp,omitempty"`
	Slack      *ConnectionSlack      `json:"slack,omitempty"`
	Telegram   *ConnectionTelegram   `json:"telegram,omitempty"`

	// DEPRECATED
	URL types.EnvVar `json:"url,omitempty"`
	// DEPRECATED
	Port types.EnvVar `json:"port,omitempty"`
	// DEPRECATED
	Type string `json:"type,omitempty"`
	// DEPRECATED
	Username types.EnvVar `json:"username,omitempty"`
	// DEPRECATED
	Password types.EnvVar `json:"password,omitempty"`
	// DEPRECATED
	Certificate types.EnvVar `json:"certificate,omitempty"`
	// DEPRECATED
	InsecureTLS bool `json:"insecure_tls,omitempty"`
}

// ConnectionStatus defines the observed state of Connection
type ConnectionStatus struct {
	// Ref is the connection string
	Ref string `json:"ref"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Connection is the Schema for the connections API
type Connection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConnectionSpec   `json:"spec,omitempty"`
	Status ConnectionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ConnectionList contains a list of Connection
type ConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Connection `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Connection{}, &ConnectionList{})
}
