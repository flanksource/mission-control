package connection

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"

	gcs "cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/containrrr/shoutrrr"
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

	"github.com/flanksource/incident-commander/k8s"
	"github.com/flanksource/incident-commander/pkg/clients/aws"
	"github.com/flanksource/incident-commander/pkg/clients/git"
)

func Test(ctx context.Context, c *models.Connection) error {
	c, err := ctx.HydrateConnection(c)
	if err != nil {
		return err
	}

	switch c.Type {
	case models.ConnectionTypeAWS:
		cc := connection.AWSConnection{
			ConnectionName: c.ID.String(),
		}
		if err := cc.Populate(ctx); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

		sess, err := aws.GetAWSConfig(&ctx, cc)
		if err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

		svc := sts.NewFromConfig(sess)
		if _, err := svc.GetCallerIdentity(ctx, nil); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeAzure:
		cred, err := azidentity.NewClientSecretCredential(c.Properties["tenant"], c.Username, c.Password, nil)
		if err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

		tokenPolicy := policy.TokenRequestOptions{
			Scopes:   []string{"https://graph.microsoft.com/.default"},
			TenantID: c.Properties["tenant"],
		}
		if _, err := cred.GetToken(ctx, tokenPolicy); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeAzureDevops:
		client := http.NewClient().
			BaseURL("https://app.vssps.visualstudio.com/_apis/profile/profiles").
			Header("Accept", "application/json").
			Auth(c.Username, c.Password)

		response, err := client.R(ctx).Get("me?api-version=7.2-preview.3")
		if err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

		if response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

	case models.ConnectionTypeDiscord:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeDynatrace:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeElasticSearch:
		client := http.NewClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("_cluster/health")
		if err != nil {
			return err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, body)
		}

	case models.ConnectionTypeEmail:
		parsed, err := url.Parse(c.URL)
		if err != nil {
			return api.Errorf(api.EINVALID, "bad shoutrrr connection url: %v", err)
		}

		queryParams := parsed.Query()
		queryParams.Set("FromAddress", c.Properties["from"])
		queryParams.Set("Subject", "Test Connection Email")
		queryParams.Set("ToAddresses", c.Properties["from"]) // Send a message to the sender itself
		parsed.RawQuery = queryParams.Encode()

		if err := shoutrrr.Send(parsed.String(), "Test Connection Email"); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeFolder:
		if _, err := os.Stat(c.Properties["path"]); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeGCP:
		opts := []option.ClientOption{option.WithCredentialsJSON([]byte(c.Certificate))}
		if endpoint, ok := c.Properties["endpoint"]; ok && endpoint != "" {
			opts = append(opts, option.WithEndpoint(endpoint))
		}

		client, err := cloudresourcemanager.NewService(ctx, opts...)
		if err != nil {
			return api.Errorf(api.EINVALID, "error creating service from credentials: %v", err)
		}

		if _, err := client.Projects.List().Do(); err != nil {
			return api.Errorf(api.EINVALID, "error listing projects: %v", err)
		}

	case models.ConnectionTypeGCS:
		session, err := gcs.NewClient(ctx.Context, option.WithEndpoint(c.Properties["endpoint"]), option.WithCredentialsJSON([]byte(c.Certificate)))
		if err != nil {
			return err
		}
		defer session.Close()

		if _, err := session.Bucket(c.Properties["bucket"]).Attrs(ctx); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeGenericWebhook:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

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
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeGithub:
		response, err := http.NewClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://api.github.com/user")
		if err != nil {
			return err
		}

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, body)
		}

	case models.ConnectionTypeGitlab:
		response, err := http.NewClient().Header("Authorization", "Bearer "+c.Password).
			R(ctx).Get("https://gitlab.com/api/v4/user")
		if err != nil {
			return err
		}

		body, _ := response.AsString()
		logger.Infof("response: %v", body)

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, "server returned (status code: %d) (msg: %s)", response.StatusCode, body)
		}

	case models.ConnectionTypeGoogleChat:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

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
			return err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, body)
		}

	case models.ConnectionTypeIFTTT:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeJMeter:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeKubernetes:
		client, err := k8s.NewClientWithConfig(c.Certificate)
		if err != nil {
			return err
		}

		if _, err := client.CoreV1().Pods("default").List(ctx, metav1.ListOptions{}); err != nil {
			return api.Errorf(api.EINVALID, "error listing pods in default namespace: %v", err)
		}

	case models.ConnectionTypeLDAP:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeMatrix:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeMattermost:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeMongo:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeMySQL:
		conn, err := sql.Open("mysql", c.URL)
		if err != nil {
			return api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeNtfy:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeOpsGenie:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypePostgres:
		pool, err := duty.NewPgxPool(c.URL)
		if err != nil {
			return api.Errorf(api.EINVALID, "error creating pgx pool: %v", err)
		}
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		if err != nil {
			return api.Errorf(api.EINVALID, "error acquiring connection: %v", err)
		}
		defer conn.Release()

		if err := conn.Ping(ctx); err != nil {
			return api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypePrometheus:
		client := http.NewClient().BaseURL(c.URL)
		if c.Username != "" || c.Password != "" {
			client = client.Auth(c.Username, c.Password)
		}

		response, err := client.R(ctx).Get("api/v1/status/config")
		if err != nil {
			return err
		}

		if !response.IsOK() {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, body)
		}

	case models.ConnectionTypePushbullet:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypePushover:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeRedis:
		rdb := redis.NewClient(&redis.Options{
			Addr:     c.URL,
			Username: c.Username,
			Password: c.Password,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeRestic:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeRocketchat:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeS3:
		cc := connection.AWSConnection{
			ConnectionName: c.ID.String(),
		}
		if err := cc.Populate(ctx); err != nil {
			return err
		}

		awsSession, err := aws.GetAWSConfig(&ctx, cc)
		if err != nil {
			return err
		}

		client := s3.NewFromConfig(awsSession, func(o *s3.Options) {
			o.UsePathStyle = c.Properties["use_path_style"] == "true"
		})
		if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: lo.ToPtr(c.Properties["bucket"])}); err != nil {
			return err
		}

	case models.ConnectionTypeSFTP:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeSlack:
		response, err := http.NewClient().R(ctx).
			Header("Authorization", fmt.Sprintf("Bearer %s", c.Password)).
			Header("Content-Type", "application/json; charset=utf-8").
			Post("https://slack.com/api/auth.test", map[string]string{"token": c.Password})
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

		responseMsg, err := response.AsJSON()
		if err != nil {
			return err
		}

		if responseMsg["ok"] != true {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, "server returned msg: %s", body)
		}

	case models.ConnectionTypeSlackWebhook:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeSMB:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeSQLServer:
		conn, err := sql.Open("sqlserver", c.URL)
		if err != nil {
			return api.Errorf(api.EINVALID, "error creating connection: %v", err)
		}
		defer conn.Close()

		if err := conn.Ping(); err != nil {
			return api.Errorf(api.EINVALID, "error pinging database: %v", err)
		}

	case models.ConnectionTypeTeams:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeTelegram:
		response, err := http.NewClient().R(ctx).Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", c.Password))
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if !response.IsOK(200) {
			body, _ := response.AsString()
			return api.Errorf(api.EINVALID, "server returned status (code %d) (msg: %s)", response.StatusCode, body)
		}

	case models.ConnectionTypeWebhook:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeWindows:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")

	case models.ConnectionTypeZulipChat:
		return api.Errorf(api.ENOTIMPLEMENTED, "not implemented")
	}

	return nil
}
