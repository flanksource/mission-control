package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func envVar(val string) types.EnvVar {
	return types.EnvVar{ValueStatic: val}
}

func envVarPtr(val string) *types.EnvVar {
	if val == "" {
		return nil
	}
	return &types.EnvVar{ValueStatic: val}
}

func envVarSecretRef(secretName, key string) types.EnvVar {
	return types.EnvVar{
		ValueFrom: &types.EnvVarSource{
			SecretKeyRef: &types.SecretKeySelector{
				LocalObjectReference: types.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}

func buildConnectionCRD(flags *connectionFlags) v1.Connection {
	conn := v1.Connection{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "mission-control.flanksource.com/v1",
			Kind:       "Connection",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      flags.Name,
			Namespace: flags.Namespace,
		},
	}

	switch flags.Type {
	case models.ConnectionTypeAWS:
		conn.Spec.AWS = buildAWSSpec(flags)

	case models.ConnectionTypeAWSKMS:
		conn.Spec.AWSKMS = &v1.ConnectionAWSKMS{
			ConnectionAWS: *buildAWSSpec(flags),
			KeyID:         flags.KeyID,
		}

	case models.ConnectionTypeS3:
		conn.Spec.S3 = &v1.ConnectionAWSS3{
			ConnectionAWS: *buildAWSSpec(flags),
			Bucket:        flags.Bucket,
			UsePathStyle:  flags.UsePathStyle,
		}

	case models.ConnectionTypeAzure:
		conn.Spec.Azure = &v1.ConnectionAzure{
			ClientID:     envVar(flags.ClientID),
			ClientSecret: envVar(flags.ClientSecret),
			TenantID:     envVar(flags.TenantID),
		}

	case models.ConnectionTypeAzureKeyVault:
		conn.Spec.AzureKeyVault = &v1.ConnectionAzureKeyVault{
			ConnectionAzure: v1.ConnectionAzure{
				ClientID:     envVar(flags.ClientID),
				ClientSecret: envVar(flags.ClientSecret),
				TenantID:     envVar(flags.TenantID),
			},
			KeyID: flags.KeyID,
		}

	case models.ConnectionTypeAzureDevops:
		conn.Spec.AzureDevops = &v1.ConnectionAzureDevops{
			URL:                 flags.URL,
			Organization:        flags.Organization,
			PersonalAccessToken: envVar(flags.PersonalAccessToken),
		}

	case models.ConnectionTypeGCP:
		conn.Spec.GCP = &v1.ConnectionGCP{
			Endpoint:    envVar(flags.URL),
			Certificate: envVar(flags.Certificate),
		}

	case models.ConnectionTypeGCS:
		conn.Spec.GCS = &v1.ConnectionGCS{
			ConnectionGCP: v1.ConnectionGCP{
				Endpoint:    envVar(flags.URL),
				Certificate: envVar(flags.Certificate),
			},
			Bucket: flags.Bucket,
		}

	case models.ConnectionTypeGCPKMS:
		conn.Spec.GCPKMS = &v1.ConnectionGCPKMS{
			ConnectionGCP: v1.ConnectionGCP{
				Endpoint:    envVar(flags.URL),
				Certificate: envVar(flags.Certificate),
			},
			KeyID: flags.KeyID,
		}

	case models.ConnectionTypePostgres:
		conn.Spec.Postgres = &v1.ConnectionPostgres{
			URL:         envVar(flags.URL),
			Host:        envVar(flags.Host),
			Username:    envVar(flags.Username),
			Password:    envVar(flags.Password),
			Database:    envVar(flags.Database),
			InsecureTLS: flags.InsecureTLS,
		}

	case models.ConnectionTypeMySQL:
		conn.Spec.MySQL = &v1.ConnectionMySQL{
			URL:         envVar(flags.URL),
			Host:        envVar(flags.Host),
			Username:    envVar(flags.Username),
			Password:    envVar(flags.Password),
			Database:    envVar(flags.Database),
			InsecureTLS: flags.InsecureTLS,
		}

	case models.ConnectionTypeSQLServer:
		conn.Spec.MSSQL = &v1.ConnectionMSSQL{
			URL:                    envVar(flags.URL),
			Host:                   envVar(flags.Host),
			Username:               envVar(flags.Username),
			Password:               envVar(flags.Password),
			Database:               envVar(flags.Database),
			TrustServerCertificate: lo.ToPtr(flags.TrustServerCertificate),
		}

	case models.ConnectionTypeMongo:
		conn.Spec.Mongo = &v1.ConnectionMongo{
			URL:         envVar(flags.URL),
			Host:        envVar(flags.Host),
			Username:    envVar(flags.Username),
			Password:    envVar(flags.Password),
			Database:    envVar(flags.Database),
			ReplicaSet:  flags.ReplicaSet,
			InsecureTLS: flags.InsecureTLS,
		}

	case models.ConnectionTypeSlack:
		conn.Spec.Slack = &v1.ConnectionSlack{
			Token:    envVar(flags.Token),
			Channel:  flags.Channel,
			BotName:  flags.BotName,
			Color:    flags.Color,
			Icon:     flags.Icon,
			ThreadTS: flags.ThreadTS,
			Title:    flags.Title,
		}

	case models.ConnectionTypeDiscord:
		conn.Spec.Discord = &v1.ConnectionDiscord{
			WebhookID: flags.WebhookID,
			Token:     flags.Token,
		}

	case models.ConnectionTypeEmail:
		conn.Spec.SMTP = &v1.ConnectionSMTP{
			Host:        flags.Host,
			Username:    envVar(flags.Username),
			Password:    envVar(flags.Password),
			Port:        flags.Port,
			FromAddress: flags.FromAddress,
			FromName:    flags.FromName,
			Subject:     flags.Subject,
			Auth:        v1.SMTPAuth(flags.Auth),
			InsecureTLS: flags.InsecureTLS,
		}

	case models.ConnectionTypeTelegram:
		conn.Spec.Telegram = &v1.ConnectionTelegram{
			Token: envVar(flags.Token),
			Chats: envVar(flags.Chats),
		}

	case models.ConnectionTypeNtfy:
		conn.Spec.Ntfy = &v1.ConnectionNtfy{
			Host:     flags.Host,
			Topic:    flags.Topic,
			Username: envVar(flags.Username),
			Password: envVar(flags.Password),
		}

	case models.ConnectionTypePushbullet:
		conn.Spec.Pushbullet = &v1.ConnectionPushbullet{
			Token:   envVar(flags.Token),
			Targets: flags.Targets,
		}

	case models.ConnectionTypePushover:
		conn.Spec.Pushover = &v1.ConnectionPushover{
			Token: envVar(flags.Token),
			User:  flags.User,
		}

	case models.ConnectionTypeHTTP:
		conn.Spec.HTTP = &v1.ConnectionHTTP{
			URL:         flags.URL,
			InsecureTLS: flags.InsecureTLS,
			Username:    envVarPtr(flags.Username),
			Password:    envVarPtr(flags.Password),
			Bearer:      envVar(flags.Bearer),
		}

	case models.ConnectionTypeGit:
		conn.Spec.Git = &v1.ConnectionGit{
			URL:         flags.URL,
			Ref:         flags.Ref,
			Certificate: envVarPtr(flags.Certificate),
			Username:    envVarPtr(flags.Username),
			Password:    envVarPtr(flags.Password),
		}

	case models.ConnectionTypeGithub:
		conn.Spec.GitHub = &v1.ConnectionGitHub{
			URL:                 flags.URL,
			PersonalAccessToken: envVar(flags.PersonalAccessToken),
		}

	case models.ConnectionTypeGitlab:
		conn.Spec.GitLab = &v1.ConnectionGitLab{
			URL:                 flags.URL,
			PersonalAccessToken: envVar(flags.PersonalAccessToken),
		}

	case models.ConnectionTypeKubernetes:
		conn.Spec.Kubernetes = &v1.ConnectionKubernetes{
			Certificate: envVar(flags.Certificate),
		}

	case models.ConnectionTypeFolder:
		conn.Spec.Folder = &v1.ConnectionFolder{
			Path: flags.Path,
		}

	case models.ConnectionTypeSFTP:
		conn.Spec.SFTP = &v1.ConnectionSFTP{
			Host:     envVar(flags.Host),
			Username: envVar(flags.Username),
			Password: envVar(flags.Password),
			Port:     flags.Port,
			Path:     flags.Path,
		}

	case models.ConnectionTypeSMB:
		conn.Spec.SMB = &v1.ConnectionSMB{
			Server:   envVar(flags.Host),
			Username: envVar(flags.Username),
			Password: envVar(flags.Password),
		}

	case models.ConnectionTypePrometheus:
		conn.Spec.Prometheus = &v1.ConnectionPrometheus{
			URL:    envVar(flags.URL),
			Bearer: envVar(flags.Bearer),
		}
		conn.Spec.Prometheus.Authentication.Username = envVar(flags.Username)
		conn.Spec.Prometheus.Authentication.Password = envVar(flags.Password)

	case models.ConnectionTypeLoki:
		conn.Spec.Loki = &v1.ConnectionLoki{
			URL:      flags.URL,
			Username: envVar(flags.Username),
			Password: envVar(flags.Password),
		}

	case models.ConnectionTypeOpenAI:
		conn.Spec.OpenAI = &v1.ConnectionOpenAI{
			ApiKey: envVar(flags.ApiKey),
		}
		if flags.URL != "" {
			conn.Spec.OpenAI.BaseURL = &types.EnvVar{ValueStatic: flags.URL}
		}
		if flags.Model != "" {
			conn.Spec.OpenAI.Model = &flags.Model
		}

	case models.ConnectionTypeAnthropic:
		conn.Spec.Anthropic = &v1.ConnectionAnthropic{
			ApiKey: envVar(flags.ApiKey),
		}
		if flags.URL != "" {
			conn.Spec.Anthropic.BaseURL = &types.EnvVar{ValueStatic: flags.URL}
		}
		if flags.Model != "" {
			conn.Spec.Anthropic.Model = &flags.Model
		}

	case models.ConnectionTypeOllama:
		conn.Spec.Ollama = &v1.ConnectionOllama{
			BaseURL: envVar(flags.URL),
			ApiKey:  envVar(flags.ApiKey),
		}
		if flags.Model != "" {
			conn.Spec.Ollama.Model = &flags.Model
		}

	case models.ConnectionTypeGemini:
		conn.Spec.Gemini = &v1.ConnectionGemini{
			ApiKey: envVar(flags.ApiKey),
		}
		if flags.Model != "" {
			conn.Spec.Gemini.Model = &flags.Model
		}

	case models.ConnectionTypeElasticSearch:
		conn.Spec.Elasticsearch = &v1.ConnectionElasticsearch{
			URL:         flags.URL,
			Username:    envVar(flags.Username),
			Password:    envVar(flags.Password),
			InsecureTLS: flags.InsecureTLS,
		}

	case models.ConnectionTypeRedis:
		conn.Spec.Redis = &v1.ConnectionRedis{
			URL:      flags.URL,
			Username: envVar(flags.Username),
			Password: envVar(flags.Password),
		}
	}

	return conn
}

func marshalConnectionCRD(conn v1.Connection) ([]byte, error) {
	// Marshal to JSON first, then clean up empty fields via map manipulation
	// to avoid noise from deprecated zero-value struct fields on ConnectionSpec
	jsonBytes, err := json.Marshal(conn)
	if err != nil {
		return nil, err
	}

	var raw map[string]any
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, err
	}

	cleanEmptyFields(raw)
	delete(raw, "status")

	if metadata, ok := raw["metadata"].(map[string]any); ok {
		delete(metadata, "creationTimestamp")
	}

	return yaml.Marshal(raw)
}

func buildAWSSpec(flags *connectionFlags) *v1.ConnectionAWS {
	aws := &v1.ConnectionAWS{
		URL:    envVar(flags.URL),
		Region: flags.Region,
	}
	if flags.FromProfile != "" {
		aws.AccessKey = envVarSecretRef(flags.Name, "AWS_ACCESS_KEY_ID")
		aws.SecretKey = envVarSecretRef(flags.Name, "AWS_SECRET_ACCESS_KEY")
		if flags.SessionToken != "" {
			aws.SessionToken = envVarSecretRef(flags.Name, "AWS_SESSION_TOKEN")
		}
	} else {
		aws.AccessKey = envVar(flags.AccessKey)
		aws.SecretKey = envVar(flags.SecretKey)
		aws.Profile = flags.Profile
	}
	return aws
}

func buildSecret(flags *connectionFlags) corev1.Secret {
	data := map[string]string{
		"AWS_ACCESS_KEY_ID":     flags.AccessKey,
		"AWS_SECRET_ACCESS_KEY": flags.SecretKey,
	}
	if flags.SessionToken != "" {
		data["AWS_SESSION_TOKEN"] = flags.SessionToken
	}
	return corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      flags.Name,
			Namespace: flags.Namespace,
		},
		StringData: data,
	}
}

func marshalDryRunOutput(flags *connectionFlags) ([]byte, error) {
	conn := buildConnectionCRD(flags)
	connYAML, err := marshalConnectionCRD(conn)
	if err != nil {
		return nil, fmt.Errorf("marshaling connection: %w", err)
	}

	if flags.FromProfile == "" {
		return connYAML, nil
	}

	secret := buildSecret(flags)
	secretJSON, err := json.Marshal(secret)
	if err != nil {
		return nil, fmt.Errorf("marshaling secret: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(secretJSON, &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling secret: %w", err)
	}
	cleanEmptyFields(raw)
	delete(raw, "status")
	if metadata, ok := raw["metadata"].(map[string]any); ok {
		delete(metadata, "creationTimestamp")
	}
	secretYAML, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling secret YAML: %w", err)
	}
	return append(secretYAML, append([]byte("---\n"), connYAML...)...), nil
}

func cleanEmptyFields(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			cleanEmptyFields(val)
			if len(val) == 0 {
				delete(m, k)
			}
		case string:
			if val == "" {
				delete(m, k)
			}
		case bool:
			if !val {
				delete(m, k)
			}
		case float64:
			if val == 0 {
				delete(m, k)
			}
		case nil:
			delete(m, k)
		case []any:
			if len(val) == 0 {
				delete(m, k)
			}
		}
	}
}
