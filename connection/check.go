package connection

import (
	"fmt"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
)

func Test(ctx context.Context, c *models.Connection) error {
	c, err := ctx.HydrateConnection(c)
	if err != nil {
		return err
	}

	switch c.Type {
	case models.ConnectionTypeAWS:
		return nil

	case models.ConnectionTypeAzure:
		return nil

	case models.ConnectionTypeAzureDevops:
		return nil

	case models.ConnectionTypeDiscord:
		return nil

	case models.ConnectionTypeDynatrace:
		return nil

	case models.ConnectionTypeElasticSearch:
		return nil

	case models.ConnectionTypeEmail:
		return nil

	case models.ConnectionTypeFolder:
		return nil

	case models.ConnectionTypeGCP:
		return nil

	case models.ConnectionTypeGCS:
		return nil

	case models.ConnectionTypeGenericWebhook:
		return nil

	case models.ConnectionTypeGit:
		return nil

	case models.ConnectionTypeGithub:
		return nil

	case models.ConnectionTypeGitlab:
		return nil

	case models.ConnectionTypeGoogleChat:
		return nil

	case models.ConnectionTypeHTTP:
		return nil

	case models.ConnectionTypeIFTTT:
		return nil

	case models.ConnectionTypeJMeter:
		return nil

	case models.ConnectionTypeKubernetes:
		return nil

	case models.ConnectionTypeLDAP:
		return nil

	case models.ConnectionTypeMatrix:
		return nil

	case models.ConnectionTypeMattermost:
		return nil

	case models.ConnectionTypeMongo:
		return nil

	case models.ConnectionTypeMySQL:
		return nil

	case models.ConnectionTypeNtfy:
		return nil

	case models.ConnectionTypeOpsGenie:
		return nil

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
		return nil

	case models.ConnectionTypePushbullet:
		return nil

	case models.ConnectionTypePushover:
		return nil

	case models.ConnectionTypeRedis:
		return nil

	case models.ConnectionTypeRestic:
		return nil

	case models.ConnectionTypeRocketchat:
		return nil

	case models.ConnectionTypeS3:
		return nil

	case models.ConnectionTypeSFTP:
		return nil

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
		return nil

	case models.ConnectionTypeSMB:
		return nil

	case models.ConnectionTypeSQLServer:
		return nil

	case models.ConnectionTypeTeams:
		return nil

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
		return nil

	case models.ConnectionTypeWindows:
		return nil

	case models.ConnectionTypeZulipChat:
		return nil

	}

	return nil
}
