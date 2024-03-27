package connection

import (
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/pkg/clients/aws"
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
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeDynatrace:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeElasticSearch:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeEmail:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeFolder:
		if _, err := os.Stat(c.Properties["path"]); err != nil {
			return api.Errorf(api.EINVALID, err.Error())
		}

	case models.ConnectionTypeGCP:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeGCS:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeGenericWebhook:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeGit:
		return fmt.Errorf("not implemented")

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
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeGoogleChat:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeHTTP:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeIFTTT:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeJMeter:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeKubernetes:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeLDAP:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeMatrix:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeMattermost:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeMongo:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeMySQL:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeNtfy:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeOpsGenie:
		return fmt.Errorf("not implemented")

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
		return fmt.Errorf("not implemented")

	case models.ConnectionTypePushbullet:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypePushover:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeRedis:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeRestic:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeRocketchat:
		return fmt.Errorf("not implemented")

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
		return fmt.Errorf("not implemented")

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
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeSMB:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeSQLServer:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeTeams:
		return fmt.Errorf("not implemented")

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
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeWindows:
		return fmt.Errorf("not implemented")

	case models.ConnectionTypeZulipChat:
		return fmt.Errorf("not implemented")
	}

	return nil
}
