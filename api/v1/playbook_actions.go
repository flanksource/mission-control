package v1

import "github.com/flanksource/duty/types"

type Labels map[string]string

type Description struct {
	// Description for the check
	Description string `yaml:"description,omitempty" json:"description,omitempty" template:"true"`
	// Name of the check
	Name string `yaml:"name" json:"name" template:"true"`
	// Icon for overwriting default icon on the dashboard
	Icon string `yaml:"icon,omitempty" json:"icon,omitempty" template:"true"`
	// Labels for the check
	Labels Labels `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Transformed checks have a delete strategy on deletion they can either be marked healthy, unhealthy or left as is
	TransformDeleteStrategy string `yaml:"transformDeleteStrategy,omitempty" json:"transformDeleteStrategy,omitempty"`
}

type Template struct {
	Template   string `yaml:"template,omitempty" json:"template,omitempty"`
	JSONPath   string `yaml:"jsonPath,omitempty" json:"jsonPath,omitempty"`
	Expression string `yaml:"expr,omitempty" json:"expr,omitempty"`
	Javascript string `yaml:"javascript,omitempty" json:"javascript,omitempty"`
}

type Templatable struct {
	Test      Template `yaml:"test,omitempty" json:"test,omitempty"`
	Display   Template `yaml:"display,omitempty" json:"display,omitempty"`
	Transform Template `yaml:"transform,omitempty" json:"transform,omitempty"`
}

type ExecAction struct {
	Description `yaml:",inline" json:",inline"`
	Templatable `yaml:",inline" json:",inline"`
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

type GCPConnection struct {
	// ConnectionName of the connection. It'll be used to populate the endpoint and credentials.
	ConnectionName string        `yaml:"connection,omitempty" json:"connection,omitempty"`
	Endpoint       string        `yaml:"endpoint" json:"endpoint,omitempty"`
	Credentials    *types.EnvVar `yaml:"credentials" json:"credentials,omitempty"`
}

type AzureConnection struct {
	ConnectionName string        `yaml:"connection,omitempty" json:"connection,omitempty"`
	ClientID       *types.EnvVar `yaml:"clientID,omitempty" json:"clientID,omitempty"`
	ClientSecret   *types.EnvVar `yaml:"clientSecret,omitempty" json:"clientSecret,omitempty"`
	TenantID       string        `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
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

type PlaybookAction struct {
	Exec ExecAction `json:"exec,omitempty" yaml:"exec,omitempty"`
}
