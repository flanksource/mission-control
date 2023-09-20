package v1

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"k8s.io/client-go/kubernetes"
)

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
	HydrateConnection(connectionName string) (*models.Connection, error)
	GetEnvValueFromCache(env types.EnvVar) (string, error)
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
	connection, err := ctx.HydrateConnection(g.ConnectionName)
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
	connection, err := ctx.HydrateConnection(g.ConnectionName)
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
		connection, err := ctx.HydrateConnection(t.ConnectionName)
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
	Name string      `yaml:"name" json:"name"`
	Exec *ExecAction `json:"exec,omitempty" yaml:"exec,omitempty"`
}
