package db

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"github.com/samber/lo"
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
		dbObj.URL = obj.Spec.AWS.URL.String()
		dbObj.Username = obj.Spec.AWS.AccessKey.String()
		dbObj.Password = obj.Spec.AWS.SecretKey.String()
		dbObj.Properties = map[string]string{
			"region":       obj.Spec.AWS.Region,
			"profile":      obj.Spec.AWS.Profile,
			"insecure_tls": strconv.FormatBool(obj.Spec.AWS.InsecureTLS),
		}
	}

	if obj.Spec.AWSKMS != nil {
		dbObj.Type = models.ConnectionTypeAWSKMS
		dbObj.URL = obj.Spec.AWSKMS.URL.String()
		dbObj.Username = obj.Spec.AWSKMS.AccessKey.String()
		dbObj.Password = obj.Spec.AWSKMS.SecretKey.String()
		dbObj.Properties = map[string]string{
			"keyID":        obj.Spec.AWSKMS.KeyID,
			"region":       obj.Spec.AWSKMS.Region,
			"profile":      obj.Spec.AWSKMS.Profile,
			"insecure_tls": strconv.FormatBool(obj.Spec.AWSKMS.InsecureTLS),
		}
	}

	if obj.Spec.S3 != nil {
		dbObj.Type = models.ConnectionTypeS3
		dbObj.URL = obj.Spec.S3.URL.String()
		dbObj.Username = obj.Spec.S3.AccessKey.String()
		dbObj.Password = obj.Spec.S3.SecretKey.String()
		dbObj.Properties = map[string]string{
			"bucket":       obj.Spec.S3.Bucket,
			"region":       obj.Spec.S3.Region,
			"profile":      obj.Spec.S3.Profile,
			"insecure_tls": strconv.FormatBool(obj.Spec.S3.InsecureTLS),
			"usePathStyle": strconv.FormatBool(obj.Spec.S3.UsePathStyle),
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

	if obj.Spec.AzureKeyVault != nil {
		dbObj.Type = models.ConnectionTypeAzureKeyVault
		dbObj.Username = obj.Spec.AzureKeyVault.ClientID.String()
		dbObj.Password = obj.Spec.AzureKeyVault.ClientSecret.String()
		dbObj.Properties = map[string]string{
			"tenant": obj.Spec.AzureKeyVault.TenantID.String(),
			"keyID":  obj.Spec.AzureKeyVault.KeyID,
		}
	}

	if obj.Spec.GCP != nil {
		dbObj.Type = models.ConnectionTypeGCP
		dbObj.Certificate = obj.Spec.GCP.Certificate.String()
		dbObj.URL = obj.Spec.GCP.Endpoint.String()
	}

	if obj.Spec.GCS != nil {
		dbObj.Type = models.ConnectionTypeGCS
		dbObj.Certificate = obj.Spec.GCS.Certificate.String()
		dbObj.URL = obj.Spec.GCS.Endpoint.String()
		dbObj.Properties = map[string]string{
			"bucket": obj.Spec.GCS.Bucket,
		}
	}

	if obj.Spec.GCPKMS != nil {
		dbObj.Type = models.ConnectionTypeGCPKMS
		dbObj.Certificate = obj.Spec.GCPKMS.Certificate.String()
		dbObj.URL = obj.Spec.GCPKMS.Endpoint.String()
		dbObj.Properties = map[string]string{
			"keyID": obj.Spec.GCPKMS.KeyID,
		}
	}

	if obj.Spec.AzureDevops != nil {
		dbObj.URL = obj.Spec.AzureDevops.URL
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
		dbObj.URL = obj.Spec.GitHub.URL
		dbObj.Type = models.ConnectionTypeGithub
		dbObj.Password = obj.Spec.GitHub.PersonalAccessToken.String()
	}

	if obj.Spec.GitLab != nil {
		dbObj.URL = obj.Spec.GitLab.URL
		dbObj.Type = models.ConnectionTypeGitlab
		dbObj.Password = obj.Spec.GitLab.PersonalAccessToken.String()
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

	if obj.Spec.Loki != nil {
		dbObj.Type = models.ConnectionTypeLoki
		dbObj.URL = obj.Spec.Loki.URL
		dbObj.Username = obj.Spec.Loki.Username.String()
		dbObj.Password = obj.Spec.Loki.Password.String()
	}

	if obj.Spec.Anthropic != nil {
		dbObj.Type = models.ConnectionTypeAnthropic
		dbObj.Password = obj.Spec.Anthropic.ApiKey.String()

		if obj.Spec.Anthropic.BaseURL != nil {
			dbObj.URL = obj.Spec.Anthropic.BaseURL.String()
		}

		if obj.Spec.Anthropic.Model != nil {
			dbObj.Properties = map[string]string{
				"model": *obj.Spec.Anthropic.Model,
			}
		}
	}

	if obj.Spec.OpenAI != nil {
		dbObj.Type = models.ConnectionTypeOpenAI
		dbObj.Password = obj.Spec.OpenAI.ApiKey.String()

		if obj.Spec.OpenAI.BaseURL != nil {
			dbObj.URL = obj.Spec.OpenAI.BaseURL.String()
		}

		if obj.Spec.OpenAI.Model != nil {
			dbObj.Properties = map[string]string{
				"model": *obj.Spec.OpenAI.Model,
			}
		}
	}

	if obj.Spec.Ollama != nil {
		dbObj.Type = models.ConnectionTypeOllama
		dbObj.URL = obj.Spec.Ollama.BaseURL.String()
		dbObj.Password = obj.Spec.Ollama.ApiKey.String()
		if obj.Spec.Ollama.Model != nil {
			dbObj.Properties = map[string]string{
				"model": *obj.Spec.Ollama.Model,
			}
		}
	}

	if obj.Spec.Gemini != nil {
		dbObj.Type = models.ConnectionTypeGemini
		dbObj.Password = obj.Spec.Gemini.ApiKey.String()
		if obj.Spec.Gemini.Model != nil {
			dbObj.Properties = map[string]string{
				"model": *obj.Spec.Gemini.Model,
			}
		}
	}

	if obj.Spec.Kubernetes != nil {
		dbObj.Type = models.ConnectionTypeKubernetes
		dbObj.Certificate = obj.Spec.Kubernetes.Certificate.String()
	}

	if obj.Spec.Mongo != nil {
		dbObj.Type = models.ConnectionTypeMongo
		dbObj.URL = obj.Spec.Mongo.URL.String()
		if dbObj.URL == "" {
			dbObj.URL = "mongodb://$(username):$(password)@$(properties.host)/?$(properties.database)"
		}
		queryParams := url.Values{}
		if obj.Spec.Mongo.ReplicaSet != "" {
			queryParams.Set("replicaSet", obj.Spec.Mongo.ReplicaSet)
		}
		if obj.Spec.Mongo.InsecureTLS {
			queryParams.Set("tls", "true")
		}
		if len(queryParams) > 0 {
			dbObj.URL += queryParams.Encode()
		}

		dbObj.Username = obj.Spec.Mongo.Username.String()
		dbObj.Password = obj.Spec.Mongo.Password.String()
		dbObj.Properties = map[string]string{
			"host":         obj.Spec.Mongo.Host.String(),
			"database":     obj.Spec.Mongo.Database.String(),
			"replica_set":  obj.Spec.Mongo.ReplicaSet,
			"insecure_tls": strconv.FormatBool(obj.Spec.Mongo.InsecureTLS),
		}
	}

	if obj.Spec.MSSQL != nil {
		dbObj.Type = models.ConnectionTypeSQLServer
		dbObj.URL = obj.Spec.MSSQL.URL.String()
		if dbObj.URL == "" {
			dbObj.URL = "sqlserver://$(username):$(password)@$(properties.host)?database=$(properties.database)"
		}

		dbObj.Username = obj.Spec.MSSQL.Username.String()
		dbObj.Password = obj.Spec.MSSQL.Password.String()
		dbObj.Properties = map[string]string{
			"host":     obj.Spec.MSSQL.Host.String(),
			"database": obj.Spec.MSSQL.Database.String(),
		}
		if obj.Spec.MSSQL.TrustServerCertificate != nil {
			dbObj.URL += "&TrustServerCertificate=$(properties.trust_server_certificate)"
			dbObj.Properties["trust_server_certificate"] = strconv.FormatBool(*obj.Spec.MSSQL.TrustServerCertificate)
		}
	}

	if obj.Spec.MySQL != nil {
		dbObj.Type = models.ConnectionTypeMySQL
		dbObj.URL = obj.Spec.MySQL.URL.String()
		if dbObj.URL == "" {
			dbObj.URL = "mysql://$(username):$(password)@$(properties.host)/$(properties.database)"
		}
		if obj.Spec.MySQL.InsecureTLS {
			dbObj.URL += "sslMode=disabled"
		}

		dbObj.Username = obj.Spec.MySQL.Username.String()
		dbObj.Password = obj.Spec.MySQL.Password.String()
		dbObj.Properties = map[string]string{
			"host":         obj.Spec.MySQL.Host.String(),
			"database":     obj.Spec.MySQL.Database.String(),
			"insecure_tls": strconv.FormatBool(obj.Spec.MySQL.InsecureTLS),
		}
	}

	if obj.Spec.Postgres != nil {
		dbObj.Type = models.ConnectionTypePostgres
		dbObj.URL = obj.Spec.Postgres.URL.String()
		if dbObj.URL == "" {
			dbObj.URL = "postgres://$(username):$(password)@$(properties.host)/$(properties.database)"
		}
		if obj.Spec.Postgres.InsecureTLS {
			dbObj.URL += "?sslmode=disable"
		}

		dbObj.Username = obj.Spec.Postgres.Username.String()
		dbObj.Password = obj.Spec.Postgres.Password.String()
		dbObj.Properties = map[string]string{
			"host":         obj.Spec.Postgres.Host.String(),
			"database":     obj.Spec.Postgres.Database.String(),
			"insecure_tls": strconv.FormatBool(obj.Spec.Postgres.InsecureTLS),
		}
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

	if obj.Spec.SMTP != nil {
		obj.Spec.SMTP.Auth, _ = lo.Coalesce(obj.Spec.SMTP.Auth, "Plain")
		obj.Spec.SMTP.Encryption, _ = lo.Coalesce(obj.Spec.SMTP.Encryption, "Auto")
		obj.Spec.SMTP.FromAddress, _ = lo.Coalesce(obj.Spec.SMTP.FromAddress, "no-reply@example.com")
		obj.Spec.SMTP.Port, _ = lo.Coalesce(obj.Spec.SMTP.Port, 25)
		if len(obj.Spec.SMTP.ToAddresses) == 0 {
			obj.Spec.SMTP.ToAddresses = []string{"no-reply@example.com"}
		}
		dbObj.URL = fmt.Sprintf("smtp://$(username):$(password)@%s:%d/?UseStartTLS=%s&Encryption=%s&Auth=%s&from=%s&to=%s",
			obj.Spec.SMTP.Host,
			obj.Spec.SMTP.Port,
			strconv.FormatBool(obj.Spec.SMTP.InsecureTLS),
			obj.Spec.SMTP.Encryption,
			obj.Spec.SMTP.Auth,
			obj.Spec.SMTP.FromAddress,
			strings.Join(obj.Spec.SMTP.ToAddresses, ","),
		)

		dbObj.Type = models.ConnectionTypeEmail
		dbObj.Username = obj.Spec.SMTP.Username.String()
		dbObj.Password = obj.Spec.SMTP.Password.String()
		dbObj.Properties = map[string]string{
			"port":     strconv.Itoa(obj.Spec.SMTP.Port),
			"subject":  obj.Spec.SMTP.Subject,
			"auth":     obj.Spec.SMTP.Auth,
			"from":     obj.Spec.SMTP.FromAddress,
			"to":       strings.Join(obj.Spec.SMTP.ToAddresses, ","),
			"fromname": obj.Spec.SMTP.FromName,
			"headers":  utils.StringMapToString(obj.Spec.SMTP.Headers),
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

	if obj.Spec.Prometheus != nil {
		dbObj.Type = models.ConnectionTypePrometheus
		dbObj.URL = obj.Spec.Prometheus.URL.String()
		dbObj.Username = obj.Spec.Prometheus.Username.String()
		dbObj.Password = obj.Spec.Prometheus.Password.String()
		dbObj.Properties = collections.MergeMap(
			obj.Spec.Prometheus.OAuth.AsProperties(),
			map[string]string{"bearer": obj.Spec.Prometheus.Bearer.String()},
		)
	}

	if obj.Spec.Slack != nil {
		dbObj.URL = "slack://$(password)@$(username)"
		dbObj.Type = models.ConnectionTypeSlack
		dbObj.Username = obj.Spec.Slack.Channel
		dbObj.Password = obj.Spec.Slack.Token.String()
		dbObj.Properties = map[string]string{
			"BotName":  obj.Spec.Slack.BotName,
			"Icon":     obj.Spec.Slack.Color,
			"ThreadTS": obj.Spec.Slack.ThreadTS,
			"Title":    obj.Spec.Slack.Title,
			"Color":    obj.Spec.Slack.Color,
		}
	}

	if obj.Spec.Telegram != nil {
		dbObj.URL = "telegram://$(password)@telegram/?Chats=$(username)"
		dbObj.Type = models.ConnectionTypeTelegram
		dbObj.Username = obj.Spec.Telegram.Chats.String()
		dbObj.Password = obj.Spec.Telegram.Token.String()
	}

	obj.Status.Ref = fmt.Sprintf("connection://%s/%s", obj.Namespace, obj.Name)
	return ctx.DB().Save(&dbObj).Error
}

func DeleteConnection(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Connection{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeleteStaleConnection(ctx context.Context, newer *v1.Connection) error {
	return ctx.DB().Model(&models.Connection{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

func ListConnections(ctx context.Context) ([]models.Connection, error) {
	var c []models.Connection
	err := ctx.DB().Omit("password", "certificate").Where("deleted_at IS NULL").Find(&c).Error
	return c, err
}
