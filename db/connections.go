package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
)

func PersistConnectionFromCRD(ctx context.Context, obj *v1.Connection) error {
	dbObj := models.Connection{
		ID:          uuid.MustParse(string(obj.GetUID())),
		Name:        obj.Name,
		Namespace:   obj.Namespace,
		Type:        obj.Spec.Type,
		URL:         obj.Spec.URL.String(),
		Username:    obj.Spec.Username.String(),
		Password:    obj.Spec.Password.String(),
		Certificate: obj.Spec.Certificate.String(),
		Properties:  obj.Spec.Properties,
		InsecureTLS: obj.Spec.InsecureTLS,
		Source:      models.SourceCRD,
		// Gorm.Save does not use defaults when inserting
		// and the timestamp used is zero time
		CreatedAt: time.Now(),
	}

	if obj.Spec.AWS != nil {
		dbObj.Type = models.ConnectionTypeAWS
		dbObj.Username = obj.Spec.AWS.AccessKey.String()
		dbObj.Password = obj.Spec.AWS.SecretKey.String()
		dbObj.Properties = map[string]string{
			"region":       obj.Spec.AWS.Region,
			"profile":      obj.Spec.AWS.Profile,
			"insecure_tls": strconv.FormatBool(obj.Spec.AWS.InsecureTLS),
		}
	}

	if obj.Spec.S3 != nil {
		dbObj.Type = models.ConnectionTypeS3
		dbObj.Username = obj.Spec.S3.AccessKey.String()
		dbObj.Password = obj.Spec.S3.SecretKey.String()
		dbObj.Properties = map[string]string{
			"bucket":       obj.Spec.S3.Bucket,
			"region":       obj.Spec.S3.Region,
			"profile":      obj.Spec.S3.Profile,
			"insecure_tls": strconv.FormatBool(obj.Spec.S3.InsecureTLS),
		}
	}

	if obj.Spec.Azure != nil {
		dbObj.Type = models.ConnectionTypeAzure
		dbObj.Username = obj.Spec.Azure.ClientID.String()
		dbObj.Password = obj.Spec.Azure.ClientSecret.String()
		dbObj.Properties = map[string]string{
			"tenant": obj.Spec.Azure.TenantID.String(),
		}
	}

	if obj.Spec.GCP != nil {
		dbObj.Type = models.ConnectionTypeGCP
		dbObj.Certificate = obj.Spec.GCP.Certificate.String()
		dbObj.URL = obj.Spec.GCP.Endpoint.String()
	}

	if obj.Spec.AzureDevops != nil {
		dbObj.Type = models.ConnectionTypeAzureDevops
		dbObj.Username = obj.Spec.AzureDevops.Organization
		dbObj.Password = obj.Spec.AzureDevops.PersonalAccessToken.String()
	}

	if obj.Spec.Folder != nil {
		dbObj.Type = models.ConnectionTypeFolder
		dbObj.Properties = map[string]string{
			"path": obj.Spec.Folder.Path,
		}
	}

	if obj.Spec.Git != nil {
		dbObj.Type = models.ConnectionTypeGit
		dbObj.URL = obj.Spec.Git.URL
		dbObj.Certificate = obj.Spec.Git.Certificate.String()
		dbObj.Username = obj.Spec.Git.Username.String()
		dbObj.Password = obj.Spec.Git.Password.String()
		dbObj.Properties = map[string]string{
			"ref": obj.Spec.Git.Ref,
		}
	}

	if obj.Spec.GitHub != nil {
		dbObj.Type = models.ConnectionTypeGithub
		dbObj.Password = obj.Spec.GitHub.PersonalAccessToken.String()
	}

	if obj.Spec.HTTP != nil {
		dbObj.Type = models.ConnectionTypeHTTP
		dbObj.URL = obj.Spec.HTTP.URL
		dbObj.Username = obj.Spec.HTTP.Username.String()
		dbObj.Password = obj.Spec.HTTP.Password.String()
		dbObj.Properties = map[string]string{
			"insecure_tls": strconv.FormatBool(obj.Spec.HTTP.InsecureTLS),
		}
	}

	if obj.Spec.Kubernetes != nil {
		dbObj.Type = models.ConnectionTypeKubernetes
		dbObj.Certificate = obj.Spec.Kubernetes.Certificate.String()
	}

	if obj.Spec.MSSQL != nil {
		dbObj.Type = models.ConnectionTypeSQLServer
		dbObj.URL = obj.Spec.MSSQL.Host.String()
		dbObj.Username = obj.Spec.MSSQL.Username.String()
		dbObj.Password = obj.Spec.MSSQL.Password.String()
	}

	if obj.Spec.Mongo != nil {
		dbObj.Type = models.ConnectionTypeMongo
		dbObj.URL = obj.Spec.Mongo.Host.String()
		dbObj.Username = obj.Spec.Mongo.Username.String()
		dbObj.Password = obj.Spec.Mongo.Password.String()
	}

	if obj.Spec.MySQL != nil {
		dbObj.Type = models.ConnectionTypeMySQL
		dbObj.URL = obj.Spec.MySQL.Host.String()
		dbObj.Username = obj.Spec.MySQL.Username.String()
		dbObj.Password = obj.Spec.MySQL.Password.String()
	}

	if obj.Spec.Postgres != nil {
		dbObj.Type = models.ConnectionTypePostgres
		dbObj.URL = obj.Spec.Postgres.Host.String()
		dbObj.Username = obj.Spec.Postgres.Username.String()
		dbObj.Password = obj.Spec.Postgres.Password.String()
	}

	if obj.Spec.SFTP != nil {
		dbObj.Type = models.ConnectionTypeSFTP
		dbObj.URL = obj.Spec.SFTP.Host.String()
		dbObj.Username = obj.Spec.SFTP.Username.String()
		dbObj.Password = obj.Spec.SFTP.Password.String()
		dbObj.Properties = map[string]string{
			"path": obj.Spec.SFTP.Path,
			"port": strconv.Itoa(obj.Spec.SFTP.Port),
		}
	}

	if obj.Spec.SMB != nil {
		dbObj.Type = models.ConnectionTypeSMB
		dbObj.URL = obj.Spec.SMB.Server.String()
		dbObj.Username = obj.Spec.SMB.Username.String()
		dbObj.Password = obj.Spec.SMB.Password.String()
		dbObj.Properties = map[string]string{
			"port": obj.Spec.SMB.Port.String(),
		}
	}

	if obj.Spec.Discord != nil {
		dbObj.URL = "discord://$(password)@$(username)"
		dbObj.Type = models.ConnectionTypeDiscord
		dbObj.Username = obj.Spec.Discord.WebhookID
		dbObj.Password = obj.Spec.Discord.Token
	}

	if obj.Spec.Email != nil {
		dbObj.URL = fmt.Sprintf("smtp://$(username):$(password)@$%s:%d/?UseStartTLS=%s&Encryption=%s&Auth=%s",
			obj.Spec.Email.Host,
			obj.Spec.Email.Port,
			strconv.FormatBool(obj.Spec.Email.InsecureTLS),
			obj.Spec.Email.Encryption,
			obj.Spec.Email.Auth,
		)
		dbObj.Type = models.ConnectionTypeEmail
		dbObj.URL = obj.Spec.Email.Host
		dbObj.Username = obj.Spec.Email.Username.String()
		dbObj.Password = obj.Spec.Email.Password.String()
		dbObj.Properties = map[string]string{
			"port":        strconv.Itoa(obj.Spec.Email.Port),
			"subject":     obj.Spec.Email.Subject,
			"auth":        obj.Spec.Email.Auth,
			"fromAddress": obj.Spec.Email.FromAddress,
			"toAddress":   strings.Join(obj.Spec.Email.ToAddresses, ", "),
		}
	}

	if obj.Spec.Ntfy != nil {
		dbObj.URL = fmt.Sprintf("ntfy://$(username):$(password)@%s/%s", obj.Spec.Ntfy.Host, obj.Spec.Ntfy.Topic)
		dbObj.Type = models.ConnectionTypeNtfy
		dbObj.Username = obj.Spec.Ntfy.Username.String()
		dbObj.Password = obj.Spec.Ntfy.Password.String()
		dbObj.Properties = map[string]string{
			"topic": obj.Spec.Ntfy.Topic,
		}
	}

	if obj.Spec.Pushbullet != nil {
		targets := strings.Join(obj.Spec.Pushbullet.Targets, ",")

		dbObj.URL = fmt.Sprintf("pushbullet://$(password)/%s", targets)
		dbObj.Type = models.ConnectionTypePushbullet
		dbObj.Password = obj.Spec.Pushbullet.Token.String()
		dbObj.Properties = map[string]string{
			"targets": targets,
		}
	}

	if obj.Spec.Pushover != nil {
		dbObj.URL = "pushover://:$(password)@$(username)"
		dbObj.Type = models.ConnectionTypePushover
		dbObj.Username = obj.Spec.Pushover.User
		dbObj.Password = obj.Spec.Pushover.Token.String()
	}

	if obj.Spec.Slack != nil {
		dbObj.URL = "slack://$(password)@$(username)"
		dbObj.Type = models.ConnectionTypeSlack
		dbObj.Username = obj.Spec.Slack.Channel
		dbObj.Password = obj.Spec.Slack.Token.String()
		dbObj.Properties = map[string]string{
			"BotName": obj.Spec.Slack.BotName,
		}
	}

	if obj.Spec.Telegram != nil {
		dbObj.URL = "telegram://$(password)@telegram/?Chats=$(username)"
		dbObj.Type = models.ConnectionTypeTelegram
		dbObj.Username = obj.Spec.Telegram.Chats.String()
		dbObj.Password = obj.Spec.Telegram.Token.String()
	}

	return ctx.DB().Save(&dbObj).Error
}

func DeleteConnection(ctx context.Context, id string) error {
	return ctx.DB().Table("connections").
		Delete(&models.Connection{}, "id = ?", id).
		Error
}
