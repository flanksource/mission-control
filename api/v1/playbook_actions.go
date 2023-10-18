package v1

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"k8s.io/client-go/kubernetes"
)

type PodAction struct {
	// Name is name of the pod that'll be created
	Name string `yaml:"name" json:"name"`
	// MaxLength is the maximum length of the logs to show
	//  Default: 3000 characters
	MaxLength int `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	// Timeout in minutes to wait for specified container to finish its job. Defaults to 5 minutes
	Timeout int `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// Spec is the container spec
	Spec json.RawMessage `yaml:"spec" json:"spec"`
}

type SQLAction struct {
	// Connection identifier e.g. connection://Postgres/flanksource
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	// URL is the database connection url
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Query is the sql query to run
	Query string `yaml:"query" json:"query" template:"true"`
	// Driver is the name of the underlying database to connect to.
	// Example: postgres, mysql, ...
	Driver string `yaml:"driver" json:"driver"`
}

type HTTPConnection struct {
	// Connection name e.g. connection://http/google
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	// Connection url, interpolated with username,password
	URL            string `yaml:"url,omitempty" json:"url,omitempty" template:"true"`
	Authentication `yaml:",inline" json:",inline"`
}

type Authentication struct {
	Username types.EnvVar `yaml:"username,omitempty" json:"username,omitempty"`
	Password types.EnvVar `yaml:"password,omitempty" json:"password,omitempty"`
}

type HTTPAction struct {
	HTTPConnection `yaml:",inline" json:",inline"`
	// Method to use - defaults to GET
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
	// NTLM when set to true will do authentication using NTLM v1 protocol
	NTLM bool `yaml:"ntlm,omitempty" json:"ntlm,omitempty"`
	// NTLM when set to true will do authentication using NTLM v2 protocol
	NTLMv2 bool `yaml:"ntlmv2,omitempty" json:"ntlmv2,omitempty"`
	// Header fields to be used in the query
	Headers []types.EnvVar `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Request Body Contents
	Body string `yaml:"body,omitempty" json:"body,omitempty" template:"true"`
	// TemplateBody controls whether the body of the request needs to be templated
	TemplateBody bool `yaml:"templateBody,omitempty" json:"templateBody,omitempty"`
}

type ExecAction struct {
	// Script can be a inline script or a path to a script that needs to be executed
	// On windows executed via powershell and in darwin and linux executed using bash
	Script      string          `yaml:"script" json:"script"`
	Connections ExecConnections `yaml:"connections,omitempty" json:"connections,omitempty"`
}

type ExecConnections struct {
	AWS   *AWSConnection   `yaml:"aws,omitempty" json:"aws,omitempty"`
	GCP   *GCPConnection   `yaml:"gcp,omitempty" json:"gcp,omitempty"`
	Azure *AzureConnection `yaml:"azure,omitempty" json:"azure,omitempty"`
}

type connectionContext interface {
	gocontext.Context
	HydratedConnectionByURL(namespace, connectionName string) (*models.Connection, error)
	GetEnvValueFromCache(env types.EnvVar, namespace string) (string, error)
}

type GCPConnection struct {
	// ConnectionName of the connection. It'll be used to populate the endpoint and credentials.
	ConnectionName string        `yaml:"connection,omitempty" json:"connection,omitempty"`
	Endpoint       string        `yaml:"endpoint" json:"endpoint,omitempty"`
	Credentials    *types.EnvVar `yaml:"credentials" json:"credentials,omitempty"`
}

// HydrateConnection attempts to find the connection by name
// and populate the endpoint and credentials.
func (g *GCPConnection) HydrateConnection(ctx connectionContext) error {
	connection, err := ctx.HydratedConnectionByURL(api.Namespace, g.ConnectionName)
	if err != nil {
		return err
	}

	if connection != nil {
		g.Credentials = &types.EnvVar{ValueStatic: connection.Certificate}
		g.Endpoint = connection.URL
	}

	return nil
}

type AzureConnection struct {
	ConnectionName string        `yaml:"connection,omitempty" json:"connection,omitempty"`
	ClientID       *types.EnvVar `yaml:"clientID,omitempty" json:"clientID,omitempty"`
	ClientSecret   *types.EnvVar `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	TenantID       string        `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
}

// HydrateConnection attempts to find the connection by name
// and populate the endpoint and credentials.
func (g *AzureConnection) HydrateConnection(ctx connectionContext) error {
	connection, err := ctx.HydratedConnectionByURL(api.Namespace, g.ConnectionName)
	if err != nil {
		return err
	}

	if connection != nil {
		g.ClientID = &types.EnvVar{ValueStatic: connection.Username}
		g.ClientSecret = &types.EnvVar{ValueStatic: connection.Password}
		g.TenantID = connection.Properties["tenantID"]
	}

	return nil
}

type AWSConnection struct {
	// ConnectionName of the connection. It'll be used to populate the endpoint, accessKey and secretKey.
	ConnectionName string       `yaml:"connection,omitempty" json:"connection,omitempty"`
	AccessKey      types.EnvVar `yaml:"accessKey" json:"accessKey,omitempty"`
	SecretKey      types.EnvVar `yaml:"secretKey" json:"secretKey,omitempty"`
	SessionToken   types.EnvVar `yaml:"sessionToken,omitempty" json:"sessionToken,omitempty"`
	Region         string       `yaml:"region,omitempty" json:"region,omitempty"`
	Endpoint       string       `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	// Skip TLS verify when connecting to aws
	SkipTLSVerify bool `yaml:"skipTLSVerify,omitempty" json:"skipTLSVerify,omitempty"`
	// glob path to restrict matches to a subset
	ObjectPath string `yaml:"objectPath,omitempty" json:"objectPath,omitempty"`
	// Use path style path: http://s3.amazonaws.com/BUCKET/KEY instead of http://BUCKET.s3.amazonaws.com/KEY
	UsePathStyle bool `yaml:"usePathStyle,omitempty" json:"usePathStyle,omitempty"`
}

// Populate populates an AWSConnection with credentials and other information.
// If a connection name is specified, it'll be used to populate the endpoint, accessKey and secretKey.
func (t *AWSConnection) Populate(ctx connectionContext, k8s kubernetes.Interface, namespace string) error {
	if t.ConnectionName != "" {
		connection, err := ctx.HydratedConnectionByURL(namespace, t.ConnectionName)
		if err != nil {
			return fmt.Errorf("could not parse EC2 access key: %v", err)
		}

		t.AccessKey.ValueStatic = connection.Username
		t.SecretKey.ValueStatic = connection.Password
		if t.Endpoint == "" {
			t.Endpoint = connection.URL
		}

		t.SkipTLSVerify = connection.InsecureTLS
		if t.Region == "" {
			if region, ok := connection.Properties["region"]; ok {
				t.Region = region
			}
		}
	}

	if accessKey, err := duty.GetEnvValueFromCache(k8s, t.AccessKey, namespace); err != nil {
		return fmt.Errorf("could not parse AWS access key id: %v", err)
	} else {
		t.AccessKey.ValueStatic = accessKey
	}

	if secretKey, err := duty.GetEnvValueFromCache(k8s, t.SecretKey, namespace); err != nil {
		return fmt.Errorf(fmt.Sprintf("Could not parse AWS secret access key: %v", err))
	} else {
		t.SecretKey.ValueStatic = secretKey
	}

	if sessionToken, err := duty.GetEnvValueFromCache(k8s, t.SessionToken, namespace); err != nil {
		return fmt.Errorf(fmt.Sprintf("Could not parse AWS session token: %v", err))
	} else {
		t.SessionToken.ValueStatic = sessionToken
	}

	return nil
}

type PlaybookAction struct {
	// delay is the parsed Delay
	delay *time.Duration `json:"-" yaml:"-"`

	// Name of the action
	Name string `yaml:"name" json:"name"`

	// Delay is the duration to delay the execution of this action.
	// The least supported value as of now is 1m.
	Delay string      `yaml:"delay,omitempty" json:"delay,omitempty"`
	Exec  *ExecAction `json:"exec,omitempty" yaml:"exec,omitempty"`
	HTTP  *HTTPAction `json:"http,omitempty" yaml:"http,omitempty"`
	SQL   *SQLAction  `json:"sql,omitempty" yaml:"sql,omitempty"`
	Pod   *PodAction  `json:"pod,omitempty" yaml:"pod,omitempty"`
}

func (p *PlaybookAction) DelayDuration() (time.Duration, error) {
	if p.delay != nil {
		return *p.delay, nil
	}

	if p.Delay == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(p.Delay)
	if err != nil {
		return 0, err
	}

	p.delay = &d
	return d, nil
}
