package v1

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"k8s.io/client-go/kubernetes"
)

type NotificationAction struct {
	// URL for the shoutrrr connection string
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Connection to use to send the notification
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	// Title of the notification
	Title string `yaml:"title" json:"title" template:"true"`
	// Message is the body of the notification
	Message string `yaml:"message" json:"message" template:"true"`
	// Properties for shoutrrr
	Properties map[string]string `yaml:"properties,omitempty" json:"properties,omitempty"`
}

type GitOpsActionRepo struct {
	// URL of the git repository
	URL string `yaml:"url" json:"url" template:"true"`
	// Branch to clone. Defaults to "main".
	Base string `yaml:"base,omitempty" json:"base,omitempty" template:"true"`
	// Branch is the new branch to create. Defaults to Base.
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty" template:"true"`
	// Connection name to use the credentials for the git repo
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty" template:"true"`
	// Type specifies the service the repository is hosted on (eg: github, gitlab, etc)
	// It is deduced from the repo URL however for private repository you can
	// explicitly specify the type manually.
	Type string `yaml:"type,omitempty" json:"type,omitempty" template:"true"`
}

type GitOpsActionCommit struct {
	AuthorName  string `yaml:"author" json:"author" template:"true"`
	AuthorEmail string `yaml:"email" json:"email" template:"true"`
	Message     string `yaml:"message" json:"message" template:"true"`
}

type GitOpsActionPR struct {
	// Title of the Pull request
	Title string `yaml:"title" json:"title" template:"true"`
	// Tags to add to the PR
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty" template:"true"`
}

type GitOpsActionPatch struct {
	Path string `yaml:"path" json:"path" template:"true"`
	YQ   string `yaml:"yq,omitempty" json:"yq,omitempty" template:"true"`
	JQ   string `yaml:"jq,omitempty" json:"jq,omitempty" template:"true"`
}

type GitOpsActionFile struct {
	Path    string `yaml:"path" json:"path" template:"true"`
	Content string `yaml:"content" json:"content" template:"true"`
}

type GitOpsAction struct {
	Repo   GitOpsActionRepo   `yaml:"repo" json:"repo" template:"true"`
	Commit GitOpsActionCommit `yaml:"commit" json:"commit" template:"true"`
	// PullRequest specifies the details for the PR to be created.
	// Only if connection type is github or azuredevops then a new PR is created.
	PullRequest *GitOpsActionPR     `yaml:"pr,omitempty" json:"pr,omitempty" template:"true"`
	Patches     []GitOpsActionPatch `yaml:"patches,omitempty" json:"patches,omitempty" template:"true"`
	// Files to create/delete.
	// Use the special "$delete" directive to delete an existing file.
	Files []GitOpsActionFile `yaml:"files,omitempty" json:"files,omitempty" template:"true"`
}

type GithubWorkflow struct {
	// Id is the workflow id or the workflow file name (eg: main.yaml)
	ID string `yaml:"id" json:"id"`

	// Ref is the git reference for the workflow.
	// The reference can be a branch or tag name.
	// 	Defaults to "main".
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty" template:"true"`

	// Input is the optional input keys and values, in JSON format, configured in the workflow file.
	Input string `yaml:"input,omitempty" json:"input,omitempty" template:"true"`
}

type GithubAction struct {
	// Repo is the name of the repository without the .git extension
	Repo string `yaml:"repo" json:"repo" template:"true"`
	// Username is the account owner of the repository. The name is not case sensitive
	Username string `yaml:"username" json:"username" template:"true"`
	// Token is the personal access token
	Token types.EnvVar `yaml:"token" json:"token"`
	// Workflows is the list of github workflows to invoke
	Workflows []GithubWorkflow `yaml:"workflows,omitempty" json:"workflows,omitempty" template:"true"`
}

type AzureDevopsPipeline struct {
	// ID is the pipeline ID
	ID string `json:"id" template:"true"`
	// Version is the pipeline version
	Version string `json:"version,omitempty" template:"true"`
}

// Ref: https://learn.microsoft.com/en-us/rest/api/azure/devops/pipelines/runs/run-pipeline?view=azure-devops-rest-7.1#request-body
type AzureDevopsPipelineParameters struct {
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// The resources the run requires.
	Resources json.RawMessage `json:"resources,omitempty" template:"true"`

	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	TemplateParameters json.RawMessage `json:"templateParameters,omitempty" template:"true"`

	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	Variables json.RawMessage `json:"variables,omitempty" template:"true"`

	StagesToSkip []string `json:"stagesToSkip,omitempty" template:"true"`
}

type AzureDevopsPipelineAction struct {
	// Org is the name of the Azure DevOps organization
	Org string `json:"org" template:"true"`
	// Project ID or project name
	Project string `json:"project" template:"true"`
	// Token is the personal access token
	Token types.EnvVar `yaml:"token" json:"token"`
	// Pipeline is the azure pipeline to invoke
	Pipeline AzureDevopsPipeline `json:"pipeline" template:"true"`
	// Parameteres are the settings that influence the pipeline run
	Parameters AzureDevopsPipelineParameters `json:"parameters,omitempty" template:"true"`
}

type PodAction struct {
	// Name is name of the pod that'll be created
	Name string `yaml:"name" json:"name"`
	// MaxLength is the maximum length of the logs to show
	//  Default: 3000 characters
	MaxLength int `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// Spec is the container spec
	Spec json.RawMessage `yaml:"spec" json:"spec"`
	// Artifacts to save
	Artifacts []Artifact `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
}

type SQLAction struct {
	// Connection identifier e.g. connection://Postgres/flanksource
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty" template:"true"`
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
	HTTPConnection `yaml:",inline" json:",inline" template:"true"`
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

type Artifact struct {
	Path string `json:"path" yaml:"path" template:"true"`
}

type GitCheckout struct {
	URL         string        `yaml:"url,omitempty" json:"url,omitempty"`
	Connection  string        `yaml:"connection,omitempty" json:"connection,omitempty"`
	Username    *types.EnvVar `yaml:"username,omitempty" json:"username,omitempty"`
	Password    *types.EnvVar `yaml:"password,omitempty" json:"password,omitempty"`
	Certificate *types.EnvVar `yaml:"certificate,omitempty" json:"certificate,omitempty"`
	// Destination is the full path to where the contents of the URL should be downloaded to.
	// If left empty, the sha256 hash of the URL will be used as the dir name.
	Destination *string `yaml:"destination,omitempty" json:"destination,omitempty"`
}

func (git GitCheckout) GetURL() types.EnvVar {
	return types.EnvVar{ValueStatic: git.URL}
}

func (git GitCheckout) GetUsername() types.EnvVar {
	return utils.Deref(git.Username)
}

func (git GitCheckout) GetPassword() types.EnvVar {
	return utils.Deref(git.Password)
}

func (git GitCheckout) GetCertificate() types.EnvVar {
	return utils.Deref(git.Certificate)
}

type ExecAction struct {
	// Script can be an inline script or a path to a script that needs to be executed
	// On windows executed via powershell and in darwin and linux executed using bash
	Script      string          `yaml:"script" json:"script" template:"true"`
	Connections ExecConnections `yaml:"connections,omitempty" json:"connections,omitempty"`
	// Artifacts to save
	Artifacts []Artifact `yaml:"artifacts,omitempty" json:"artifacts,omitempty" template:"true"`
	// EnvVars are the environment variables that are accessible to exec processes
	EnvVars []types.EnvVar `yaml:"env,omitempty" json:"env,omitempty"`
	// Checkout details the git repository that should be mounted to the process
	Checkout *GitCheckout `yaml:"checkout,omitempty" json:"checkout,omitempty"`
}

type ExecConnections struct {
	AWS   *AWSConnection   `yaml:"aws,omitempty" json:"aws,omitempty"`
	GCP   *GCPConnection   `yaml:"gcp,omitempty" json:"gcp,omitempty"`
	Azure *AzureConnection `yaml:"azure,omitempty" json:"azure,omitempty"`
}

type connectionContext interface {
	gocontext.Context
	HydrateConnectionByURL(connectionName string) (*models.Connection, error)
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
	connection, err := ctx.HydrateConnectionByURL(g.ConnectionName)
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
	connection, err := ctx.HydrateConnectionByURL(g.ConnectionName)
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
		connection, err := ctx.HydrateConnectionByURL(t.ConnectionName)
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
		return fmt.Errorf("could not parse AWS secret access key: %w", err)
	} else {
		t.SecretKey.ValueStatic = secretKey
	}

	if sessionToken, err := duty.GetEnvValueFromCache(k8s, t.SessionToken, namespace); err != nil {
		return fmt.Errorf("could not parse AWS session token: %w", err)
	} else {
		t.SessionToken.ValueStatic = sessionToken
	}

	return nil
}

type PlaybookAction struct {
	// delay is the parsed Delay
	delay *time.Duration `json:"-" yaml:"-"`

	// timeout is the parsed Timeout
	timeout *time.Duration `json:"-" yaml:"-"`

	PlaybookID string `json:"-" yaml:"-"`

	// Name of the action
	Name string `yaml:"name" json:"name"`

	// Delay is a CEL expression that returns the duration to delay the execution of this action.
	// Valid time units are "s", "m", "h", "d", "w", "y".
	// It's only sensitive to the minute. i.e. if you delay by 20s it can take upto a minute to execute.
	Delay string `yaml:"delay,omitempty" json:"delay,omitempty"`

	// Timeout is the maximum duration to let an action run before it's cancelled.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Filter is a cel-expression that decides if this action should run or not.
	// The expressions should either return a boolean value ('true' indicating run the action & vice versa)
	// or any of these special functions.
	// Examples:
	// 	- filter: config.deleted_at ? true: false
	// 	- filter: always()
	// always(): run no matter what; even if the playbook is cancelled/fails
	// failure(): run if any of the previous actions failed
	// success(): run only if all previous actions succeeded (default)
	// timeout(): run only if any of the previous actions timed out
	// skip(): skip running this action
	Filter string `yaml:"if,omitempty" json:"if,omitempty"`

	// RunsOn specifies the agents that can run this action.
	// When left empty, the action will run on the main instance itself.
	RunsOn []string `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`

	// TemplatesOn specifies where the templating happens.
	// Available options:
	//  - host
	//  - agent
	// When left empty, the templating is done on the main instance(host) itself.
	TemplatesOn string `json:"templatesOn,omitempty" yaml:"templatesOn,omitempty"`

	Exec                *ExecAction                `json:"exec,omitempty" yaml:"exec,omitempty" template:"true"`
	GitOps              *GitOpsAction              `json:"gitops,omitempty" yaml:"gitops,omitempty" template:"true"`
	Github              *GithubAction              `json:"github,omitempty" yaml:"github,omitempty" template:"true"`
	AzureDevopsPipeline *AzureDevopsPipelineAction `json:"azureDevopsPipeline,omitempty" yaml:"azureDevops,omitempty" template:"true"`
	HTTP                *HTTPAction                `json:"http,omitempty" yaml:"http,omitempty" template:"true"`
	SQL                 *SQLAction                 `json:"sql,omitempty" yaml:"sql,omitempty" template:"true"`
	Pod                 *PodAction                 `json:"pod,omitempty" yaml:"pod,omitempty" template:"true"`
	Notification        *NotificationAction        `json:"notification,omitempty" yaml:"notification,omitempty" template:"true"`
}

func (p *PlaybookAction) Context() map[string]any {
	return map[string]any{
		"action_name": p.Name,
	}
}

func (p *PlaybookAction) DelayDuration() (time.Duration, error) {
	if p.delay != nil {
		return *p.delay, nil
	}

	if p.Delay == "" {
		return 0, nil
	}

	d, err := duration.ParseDuration(p.Delay)
	if err != nil {
		return 0, err
	}

	p.delay = utils.Ptr(time.Duration(d))
	return time.Duration(d), nil
}

func (p *PlaybookAction) TimeoutDuration() (time.Duration, error) {
	if p.timeout != nil {
		return *p.timeout, nil
	}

	if p.Timeout == "" {
		return 0, nil
	}

	d, err := duration.ParseDuration(p.Timeout)
	if err != nil {
		return 0, err
	}

	p.timeout = utils.Ptr(time.Duration(d))
	return time.Duration(d), nil
}
