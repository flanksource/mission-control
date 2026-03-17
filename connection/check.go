package connection

import (
	"database/sql"
	"fmt"
	nethttp "net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/flanksource/artifacts"
	"github.com/flanksource/commons/har"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	dutyKubernetes "github.com/flanksource/duty/kubernetes"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	_ "github.com/go-sql-driver/mysql"
	redis "github.com/redis/go-redis/v9"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/option"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/pkg/clients/git"
)

type TestResult struct {
	Payload map[string]any `json:"payload,omitempty"`
	Entries []har.Entry    `json:"entries,omitempty"`
}

// azureHARTransport adapts net/http.RoundTripper to Azure's policy.Transporter interface.
type azureHARTransport struct {
	rt nethttp.RoundTripper
}

func (a *azureHARTransport) Do(req *nethttp.Request) (*nethttp.Response, error) {
	return a.rt.RoundTrip(req)
}

func Test(ctx context.Context, c *models.Connection) (TestResult, error) {
	collector := har.NewCollector(har.DefaultConfig())
	harOpt := types.WithHARCollector(collector)
	newHTTPClient := func() *http.Client {
		return http.NewClient().HARCollector(collector)
	}
	harTransport := collector.Middleware()(nethttp.DefaultTransport)

	switch c.Type {
	case models.ConnectionTypeAWS:
		cc := connection.AWSConnection{}
		cc.FromModel(*c)

		sess, err := cc.Client(ctx, harOpt)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "%v", err)
		}

		svc := sts.NewFromConfig(sess)
		identity, err := svc.GetCallerIdentity(ctx, nil)
		if err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%v", err)
		}

		return TestResult{
			Payload: map[string]any{
				"Account": ptrVal(identity.Account),
				"Arn":     ptrVal(identity.Arn),
				"UserId":  ptrVal(identity.UserId),
			},
			Entries: collector.Entries(),
		}, nil

	case models.ConnectionTypeAzure:
		cred, err := azidentity.NewClientSecretCredential(c.Properties["tenant"], c.Username, c.Password, &azidentity.ClientSecretCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: &azureHARTransport{rt: harTransport},
			},
		})
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "%v", err)
		}

		tokenPolicy := policy.TokenRequestOptions{
			Scopes:   []string{"https://graph.microsoft.com/.default"},
			TenantID: c.Properties["tenant"],
		}
		if _, err := cred.GetToken(ctx, tokenPolicy); err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%v", err)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeAzureDevops:
		client := newHTTPClient().
			BaseURL("https://app.vssps.visualstudio.com/_apis/profile/profiles").
			Header("Accept", "application/json").
			Auth(c.Username, c.Password)

		response, err := client.R(ctx).Get("me?api-version=7.2-preview.3")
		if err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%v", err)
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeFacet:
		reqBody := map[string]any{
			"code":   `export default function Test() { return <div>Connection Test</div> }`,
			"format": "pdf",
		}
		if timestampURL := c.Properties["timestampUrl"]; timestampURL != "" {
			reqBody["signature"] = map[string]string{
				"selfSigned":   "true",
				"timestampUrl": timestampURL,
			}
		}
		client := newHTTPClient().BaseURL(c.URL)
		if c.Password != "" {
			client = client.Header("X-API-Key", c.Password)
		}
		response, err := client.R(ctx).Post("/render", reqBody)
		if err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "facet render request failed: %v", err)
		}
		if !response.IsOK() {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "facet render failed (status %d): %s", response.StatusCode, body)
		}

		renderResult, err := response.AsJSON()
		if err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "failed to parse render response: %v", err)
		}
		resultURL, _ := renderResult["url"].(string)
		if resultURL == "" {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "render response missing 'url' field")
		}

		pdfResponse, err := client.R(ctx).Get(resultURL)
		if err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "failed to fetch rendered PDF: %v", err)
		}
		if !pdfResponse.IsOK() {
			body, _ := pdfResponse.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "PDF fetch failed (status %d): %s", pdfResponse.StatusCode, body)
		}

		return TestResult{
			Payload: map[string]any{"resultUrl": resultURL},
			Entries: collector.Entries(),
		}, nil

	case models.ConnectionTypeElasticSearch:
		client := newHTTPClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("_cluster/health")
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%s", body)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeEmail:
		var conn v1.ConnectionSMTP
		if err := conn.FromURL(c.URL); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "bad shoutrrr connection url: %v", err)
		}

		sender := c.Properties["from"]
		if sender == "" {
			sender = conn.FromAddress
		}
		subject := "Test Connection Email | Flanksource Mission Control"

		m := mail.New([]string{sender}, subject, "test", "text/plain")
		m.SetFrom("Flanksource Test", sender)
		m.SetCredentials(conn.Host, conn.Port, c.Username, c.Password)
		if err := m.Send(conn); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "%v", err)
		}

		return TestResult{Payload: map[string]any{
			"subject":    subject,
			"from":       sender,
			"to":         []string{sender},
			"host":       conn.Host,
			"port":       conn.Port,
			"encryption": conn.Encryption,
			"auth":       conn.Auth,
		}}, nil

	case models.ConnectionTypeFolder:
		return testArtifactConnection(ctx, c)

	case models.ConnectionTypeGCP:
		opts := []option.ClientOption{
			option.WithCredentialsJSON([]byte(c.Certificate)),
			option.WithHTTPClient(&nethttp.Client{Transport: harTransport}),
		}
		if endpoint, ok := c.Properties["endpoint"]; ok && endpoint != "" {
			opts = append(opts, option.WithEndpoint(endpoint))
		}

		client, err := cloudresourcemanager.NewService(ctx, opts...)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating service from credentials: %v", err)
		}

		if _, err := client.Projects.List().Do(); err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "error listing projects: %v", err)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeGCS:
		return testArtifactConnection(ctx, c)

	case models.ConnectionTypeGit:
		_, _, err := git.Clone(ctx, &git.GitopsAPISpec{
			Repository:    c.URL,
			Branch:        c.Properties["ref"],
			Base:          c.Properties["ref"],
			User:          c.Username,
			Password:      c.Password,
			SSHPrivateKey: c.Certificate,
		})
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeGithub:
		response, err := newHTTPClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://api.github.com/user")
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%s", body)
		}

		body, err := response.AsJSON()
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		return TestResult{
			Payload: map[string]any{
				"login":   body["login"],
				"id":      body["id"],
				"name":    body["name"],
				"email":   body["email"],
				"scopes":  response.Header.Get("X-OAuth-Scopes"),
				"rate":    response.Header.Get("X-RateLimit-Limit"),
				"rateRem": response.Header.Get("X-RateLimit-Remaining"),
			},
			Entries: collector.Entries(),
		}, nil

	case models.ConnectionTypeGitlab:
		response, err := newHTTPClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://gitlab.com/api/v4/user")
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "server returned (status code: %d) (msg: %s)", response.StatusCode, body)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeHTTP:
		httpConn, err := connection.NewHTTPConnection(ctx, *c)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating HTTP connection: %v", err)
		}

		hydrated, err := httpConn.Hydrate(ctx, c.Namespace)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error hydrating HTTP connection: %v", err)
		}

		client, err := connection.CreateHTTPClient(ctx, *hydrated)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating HTTP client: %v", err)
		}

		client = client.HARCollector(collector)

		if c.InsecureTLS {
			client = client.InsecureSkipVerify(true)
		}

		response, err := client.R(ctx).Get(c.URL)
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%s", body)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeKubernetes:
		client, _, err := dutyKubernetes.NewClientFromPathOrConfig(ctx.Logger, c.Certificate, collector)
		if err != nil {
			return TestResult{}, err
		}

		if _, err := client.Discovery().ServerVersion(); err != nil {
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "failed to reach kubernetes API server: %v", err)
		}

		return TestResult{Entries: collector.Entries()}, nil

	case models.ConnectionTypeMySQL:
		conn, err := sql.Open("mysql", c.URL)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeOpenSearch:
		var conn connection.OpensearchConnection
		if err := conn.FromModel(*c); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		client, err := conn.Client()
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating client: %v", err)
		}

		r, err := client.Ping()
		if err != nil || r.IsError() {
			return TestResult{}, api.Errorf(api.EINVALID, "error ping opensearch: %v, %v", err, r)
		}

	case models.ConnectionTypePostgres:
		pool, err := duty.NewPgxPool(c.URL)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating pgx pool: %v", err)
		}
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error acquiring connection: %v", err)
		}
		defer conn.Release()

		if err := conn.Ping(ctx); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

		var version, database, user string
		if err := conn.QueryRow(ctx, "select version(), current_database(), current_user").Scan(&version, &database, &user); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error querying connection: %v", err)
		}

		return TestResult{Payload: map[string]any{
			"version":  version,
			"database": database,
			"user":     user,
		}}, nil

	case models.ConnectionTypePrometheus:
		client := newHTTPClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("api/v1/query?query=sum(up)")
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "%s", body)
		}

		payload, err := response.AsJSON()
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		return TestResult{Payload: payload, Entries: collector.Entries()}, nil

	case models.ConnectionTypeRedis:
		rdb := redis.NewClient(&redis.Options{
			Addr:     c.URL,
			Username: c.Username,
			Password: c.Password,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeS3:
		return testArtifactConnection(ctx, c)

	case models.ConnectionTypeSlack:
		response, err := newHTTPClient().R(ctx).
			Header("Authorization", fmt.Sprintf("Bearer %s", c.Password)).
			Header("Content-Type", "application/json; charset=utf-8").
			Post("https://slack.com/api/auth.test", map[string]string{"token": c.Password})
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

		responseMsg, err := response.AsJSON()
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}

		payload := map[string]any{
			"team":     responseMsg["team"],
			"team_id":  responseMsg["team_id"],
			"user":     responseMsg["user"],
			"user_id":  responseMsg["user_id"],
			"url":      responseMsg["url"],
			"bot_id":   responseMsg["bot_id"],
			"bot_user": responseMsg["bot_user_id"],
		}

		if c.Username != "" {
			postResponse, err := newHTTPClient().R(ctx).
				Header("Authorization", fmt.Sprintf("Bearer %s", c.Password)).
				Header("Content-Type", "application/json; charset=utf-8").
				Post("https://slack.com/api/chat.postMessage", map[string]any{
					"text":    "Test message from mission control",
					"channel": c.Username,
				})
			if err != nil {
				return TestResult{Payload: payload, Entries: collector.Entries()}, err
			}
			defer postResponse.Body.Close()

			if !postResponse.IsOK(200) {
				body, _ := postResponse.AsString()
				return TestResult{Payload: payload, Entries: collector.Entries()}, api.Errorf(api.EINVALID, "failed to check channel access (code %d) (msg: %s)", postResponse.StatusCode, body)
			}

			if response, err := postResponse.AsJSON(); err != nil {
				return TestResult{Payload: payload, Entries: collector.Entries()}, err
			} else if response["ok"] != true {
				return TestResult{Payload: payload, Entries: collector.Entries()}, api.Errorf(api.EINVALID, "bot does not have access to channel %s: %v", c.Username, response["error"])
			} else {
				payload["channel"] = c.Username
				payload["post_ts"] = response["ts"]
				payload["post_channel"] = response["channel"]
			}
		}

		return TestResult{Payload: payload, Entries: collector.Entries()}, nil

	case models.ConnectionTypeSQLServer:
		conn, err := sql.Open("sqlserver", c.URL)
		if err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return TestResult{}, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeTelegram:
		response, err := newHTTPClient().R(ctx).Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", c.Password))
		if err != nil {
			return TestResult{Entries: collector.Entries()}, err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return TestResult{Entries: collector.Entries()}, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

		return TestResult{Entries: collector.Entries()}, nil

	default:
		return TestResult{}, api.Errorf(api.ENOTIMPLEMENTED, "Testing %s connection is not available", c.Type)
	}

	return TestResult{Entries: collector.Entries()}, nil
}

func testArtifactConnection(ctx context.Context, c *models.Connection) (TestResult, error) {
	store, err := artifacts.GetFSForConnection(ctx, *c)
	if err != nil {
		return TestResult{}, api.Errorf(api.EINVALID, "error creating filesystem: %v", err)
	}
	defer store.Close()

	testPath := ".mission-control-test"
	info, err := store.Write(ctx, testPath, strings.NewReader("connection test"))
	if err != nil {
		return TestResult{}, api.Errorf(api.EINVALID, "error writing test file: %v", err)
	}

	payload := map[string]any{
		"path": testPath,
		"size": info.Size(),
	}

	reader, err := store.Read(ctx, testPath)
	if err != nil {
		return TestResult{Payload: payload}, api.Errorf(api.EINVALID, "error reading test file: %v", err)
	}
	reader.Close()

	return TestResult{Payload: payload}, nil
}

func ptrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
