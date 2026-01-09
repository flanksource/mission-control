package connection

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"strings"

	gcs "cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	_ "github.com/go-sql-driver/mysql"
	redis "github.com/redis/go-redis/v9"
	"github.com/samber/lo"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/option"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/k8s"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/pkg/clients/aws"
	"github.com/flanksource/incident-commander/pkg/clients/git"
)

const smtpDebugLimit = 8000

func Test(ctx context.Context, c *models.Connection) (map[string]any, error) {
	c, err := ctx.HydrateConnection(c)
	if err != nil {
		return nil, err
	}

	switch c.Type {
	case models.ConnectionTypeAWS:
		cc := connection.AWSConnection{
			ConnectionName: c.ID.String(),
		}
		if err := cc.Populate(ctx); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

		sess, err := aws.GetAWSConfig(&ctx, cc)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

		svc := sts.NewFromConfig(sess)
		if _, err := svc.GetCallerIdentity(ctx, nil); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeAzure:
		cred, err := azidentity.NewClientSecretCredential(c.Properties["tenant"], c.Username, c.Password, nil)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

		tokenPolicy := policy.TokenRequestOptions{
			Scopes:   []string{"https://graph.microsoft.com/.default"},
			TenantID: c.Properties["tenant"],
		}
		if _, err := cred.GetToken(ctx, tokenPolicy); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeAzureDevops:
		client := http.NewClient().
			BaseURL("https://app.vssps.visualstudio.com/_apis/profile/profiles").
			Header("Accept", "application/json").
			Auth(c.Username, c.Password)

		response, err := client.R(ctx).Get("me?api-version=7.2-preview.3")
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

		if response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

	case models.ConnectionTypeElasticSearch:
		client := http.NewClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("_cluster/health")
		if err != nil {
			return nil, err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "%s", body)
		}

	case models.ConnectionTypeEmail:
		var conn v1.ConnectionSMTP
		if err := conn.FromURL(c.URL); err != nil {
			return nil, api.Errorf(api.EINVALID, "bad shoutrrr connection url: %v", err)
		}

		sender := c.Properties["from"]
		if sender == "" {
			sender = conn.FromAddress
		}
		subject := "Test Connection Email | Flanksource Mission Control"

		var debug bytes.Buffer
		m := mail.New([]string{sender}, subject, "test", "text/plain").
			SetDebugWriter(&debug)
		m.SetFrom("Flanksource Test", sender)
		m.SetCredentials(conn.Host, conn.Port, c.Username, c.Password)
		if err := m.Send(conn); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

		payload := map[string]any{
			"subject":    subject,
			"from":       sender,
			"to":         []string{sender},
			"host":       conn.Host,
			"port":       conn.Port,
			"encryption": conn.Encryption,
			"auth":       conn.Auth,
		}
		if debug.Len() > 0 {
			payload["debug"] = truncateSMTPDebug(scrubSMTPDebug(debug.String()))
		}
		return payload, nil

	case models.ConnectionTypeFolder:
		if _, err := os.Stat(c.Properties["path"]); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeGCP:
		opts := []option.ClientOption{option.WithCredentialsJSON([]byte(c.Certificate))}
		if endpoint, ok := c.Properties["endpoint"]; ok && endpoint != "" {
			opts = append(opts, option.WithEndpoint(endpoint))
		}

		client, err := cloudresourcemanager.NewService(ctx, opts...)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating service from credentials: %v", err)
		}

		if _, err := client.Projects.List().Do(); err != nil {
			return nil, api.Errorf(api.EINVALID, "error listing projects: %v", err)
		}

	case models.ConnectionTypeGCS:
		session, err := gcs.NewClient(ctx.Context, option.WithEndpoint(c.Properties["endpoint"]), option.WithCredentialsJSON([]byte(c.Certificate)))
		if err != nil {
			return nil, err
		}
		defer session.Close()

		if _, err := session.Bucket(c.Properties["bucket"]).Attrs(ctx); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

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
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeGithub:
		response, err := http.NewClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://api.github.com/user")
		if err != nil {
			return nil, err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "%s", body)
		}

		body, err := response.AsJSON()
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"login":   body["login"],
			"id":      body["id"],
			"name":    body["name"],
			"email":   body["email"],
			"scopes":  response.Header.Get("X-OAuth-Scopes"),
			"rate":    response.Header.Get("X-RateLimit-Limit"),
			"rateRem": response.Header.Get("X-RateLimit-Remaining"),
		}, nil

	case models.ConnectionTypeGitlab:
		response, err := http.NewClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://gitlab.com/api/v4/user")
		if err != nil {
			return nil, err
		}

		body, _ := response.AsString()
		logger.Infof("response: %v", body)

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "server returned (status code: %d) (msg: %s)", response.StatusCode, body)
		}

	case models.ConnectionTypeHTTP:
		client := http.NewClient()
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		if c.Properties["insecure_tls"] == "true" {
			client = client.InsecureSkipVerify(true)
		}

		response, err := client.R(ctx).Get(c.URL)
		if err != nil {
			return nil, err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "%s", body)
		}

	case models.ConnectionTypeKubernetes:
		client, err := k8s.NewClientWithConfig(c.Certificate)
		if err != nil {
			return nil, err
		}

		if _, err := client.CoreV1().Pods("default").List(ctx, metav1.ListOptions{}); err != nil {
			return nil, api.Errorf(api.EINVALID, "error listing pods in default namespace: %v", err)
		}

	case models.ConnectionTypeMySQL:
		conn, err := sql.Open("mysql", c.URL)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return nil, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeOpenSearch:
		var conn connection.OpensearchConnection
		if err := conn.FromModel(*c); err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		client, err := conn.Client()
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating client: %v", err)
		}

		r, err := client.Ping()
		if err != nil || r.IsError() {
			return nil, api.Errorf(api.EINVALID, "error ping opensearch: %v, %v", err, r)
		}

	case models.ConnectionTypePostgres:
		pool, err := duty.NewPgxPool(c.URL)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating pgx pool: %v", err)
		}
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error acquiring connection: %v", err)
		}
		defer conn.Release()

		if err := conn.Ping(ctx); err != nil {
			return nil, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

		var version, database, user string
		if err := conn.QueryRow(ctx, "select version(), current_database(), current_user").Scan(&version, &database, &user); err != nil {
			return nil, api.Errorf(api.EINVALID, "error querying connection: %v", err)
		}

		return map[string]any{
			"version":  version,
			"database": database,
			"user":     user,
		}, nil

	case models.ConnectionTypePrometheus:
		client := http.NewClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("api/v1/query?query=sum(up)")
		if err != nil {
			return nil, err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "%s", body)
		}

		payload, err := response.AsJSON()
		if err != nil {
			return nil, err
		}

		return payload, nil

	case models.ConnectionTypeRedis:
		rdb := redis.NewClient(&redis.Options{
			Addr:     c.URL,
			Username: c.Username,
			Password: c.Password,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return nil, api.Errorf(api.EINVALID, "%v", err)
		}

	case models.ConnectionTypeS3:
		cc := connection.AWSConnection{
			ConnectionName: c.ID.String(),
		}
		if err := cc.Populate(ctx); err != nil {
			return nil, err
		}

		awsSession, err := aws.GetAWSConfig(&ctx, cc)
		if err != nil {
			return nil, err
		}

		client := s3.NewFromConfig(awsSession, func(o *s3.Options) {
			o.UsePathStyle = c.Properties["use_path_style"] == "true"
		})
		if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: lo.ToPtr(c.Properties["bucket"])}); err != nil {
			return nil, err
		}

	case models.ConnectionTypeSlack:
		response, err := http.NewClient().R(ctx).
			Header("Authorization", fmt.Sprintf("Bearer %s", c.Password)).
			Header("Content-Type", "application/json; charset=utf-8").
			Post("https://slack.com/api/auth.test", map[string]string{"token": c.Password})
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

		responseMsg, err := response.AsJSON()
		if err != nil {
			return nil, err
		}

		if responseMsg["ok"] != true {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "server returned msg: %s", body)
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
			// Ensure the bot has permission on the channel
			postResponse, err := http.NewClient().R(ctx).
				Header("Authorization", fmt.Sprintf("Bearer %s", c.Password)).
				Header("Content-Type", "application/json; charset=utf-8").
				Post("https://slack.com/api/chat.postMessage", map[string]any{
					"text":    "Test message from mission control",
					"channel": c.Username,
				})
			if err != nil {
				return nil, err
			}
			defer postResponse.Body.Close()

			if !postResponse.IsOK(200) {
				body, _ := postResponse.AsString()
				return nil, api.Errorf(api.EINVALID, "failed to check channel access (code %d) (msg: %s)", postResponse.StatusCode, body)
			}

			if response, err := postResponse.AsJSON(); err != nil {
				return nil, err
			} else if response["ok"] != true {
				return nil, api.Errorf(api.EINVALID, "bot does not have access to channel %s: %v", c.Username, response["error"])
			} else {
				payload["channel"] = c.Username
				payload["post_ts"] = response["ts"]
				payload["post_channel"] = response["channel"]
			}
		}

		return payload, nil

	case models.ConnectionTypeSQLServer:
		conn, err := sql.Open("sqlserver", c.URL)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return nil, api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeTelegram:
		response, err := http.NewClient().R(ctx).Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", c.Password))
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return nil, api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

	default:
		return nil, api.Errorf(api.ENOTIMPLEMENTED, "Testing %s connection is not available", c.Type)
	}

	return nil, nil
}

func scrubSMTPDebug(raw string) string {
	lines := strings.Split(raw, "\n")
	redactNext := 0
	for i, line := range lines {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "AUTH ") {
			lines[i] = "C: AUTH <redacted>"
			if strings.Contains(upper, "AUTH LOGIN") {
				redactNext = 2
			}
			continue
		}
		if redactNext > 0 && strings.HasPrefix(strings.TrimSpace(line), "C:") {
			lines[i] = "C: <redacted>"
			redactNext--
		}
	}
	return strings.Join(lines, "\n")
}

func truncateSMTPDebug(value string) string {
	if len(value) <= smtpDebugLimit {
		return value
	}
	return value[:smtpDebugLimit] + "\n...truncated..."
}
