package clientcmd

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/clientapi"
	"sigs.k8s.io/yaml"
)

type manifestMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type connectionManifest struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   manifestMetadata `json:"metadata"`
	Spec       map[string]any   `json:"spec"`
}

type secretManifest struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   manifestMetadata  `json:"metadata"`
	StringData map[string]string `json:"stringData"`
}

func envVar(value string) clientapi.EnvVar {
	return clientapi.EnvVar{ValueStatic: value}
}

func envVarPtr(value string) *clientapi.EnvVar {
	if value == "" {
		return nil
	}
	return &clientapi.EnvVar{ValueStatic: value}
}

func envVarSecretRef(secretName, key string) clientapi.EnvVar {
	return clientapi.EnvVar{
		ValueFrom: &clientapi.EnvVarSource{
			SecretKeyRef: &clientapi.SecretKeySelector{Name: secretName, Key: key},
		},
	}
}

func buildConnectionCRD(flags *ConnectionFlags) connectionManifest {
	manifest := connectionManifest{
		APIVersion: "mission-control.flanksource.com/v1",
		Kind:       "Connection",
		Metadata:   manifestMetadata{Name: flags.Name, Namespace: flags.Namespace},
		Spec:       make(map[string]any),
	}

	switch flags.Type {
	case clientapi.ConnectionTypeAWS:
		manifest.Spec["aws"] = buildAWSSpec(flags)
	case clientapi.ConnectionTypeAWSKMS:
		spec := buildAWSSpec(flags)
		spec["keyID"] = flags.KeyID
		manifest.Spec["awskms"] = spec
	case clientapi.ConnectionTypeS3:
		spec := buildAWSSpec(flags)
		spec["bucket"] = flags.Bucket
		spec["usePathStyle"] = flags.UsePathStyle
		manifest.Spec["s3"] = spec
	case clientapi.ConnectionTypeAzure:
		manifest.Spec["azure"] = buildAzureSpec(flags)
	case clientapi.ConnectionTypeAzureKeyVault:
		spec := buildAzureSpec(flags)
		spec["keyID"] = flags.KeyID
		manifest.Spec["azureKeyVault"] = spec
	case clientapi.ConnectionTypeAzureDevops:
		manifest.Spec["azureDevops"] = map[string]any{
			"url": flags.URL, "organization": flags.Organization, "personalAccessToken": envVar(flags.PersonalAccessToken),
		}
	case clientapi.ConnectionTypeGCP:
		manifest.Spec["gcp"] = buildGCPSpec(flags)
	case clientapi.ConnectionTypeGCS:
		spec := buildGCPSpec(flags)
		spec["bucket"] = flags.Bucket
		manifest.Spec["gcs"] = spec
	case clientapi.ConnectionTypeGCPKMS:
		spec := buildGCPSpec(flags)
		spec["keyID"] = flags.KeyID
		manifest.Spec["gcpkms"] = spec
	case clientapi.ConnectionTypePostgres:
		manifest.Spec["postgres"] = buildDatabaseSpec(flags, true)
	case clientapi.ConnectionTypeMySQL:
		manifest.Spec["mysql"] = buildDatabaseSpec(flags, true)
	case clientapi.ConnectionTypeSQLServer:
		spec := buildDatabaseSpec(flags, false)
		spec["trustServerCertificate"] = flags.TrustServerCertificate
		manifest.Spec["mssql"] = spec
	case clientapi.ConnectionTypeMongo:
		spec := buildDatabaseSpec(flags, true)
		spec["replicaSet"] = flags.ReplicaSet
		manifest.Spec["mongo"] = spec
	case clientapi.ConnectionTypeSlack:
		manifest.Spec["slack"] = map[string]any{
			"token": envVar(flags.Token), "channel": flags.Channel, "botName": flags.BotName,
			"color": flags.Color, "icon": flags.Icon, "thread_ts": flags.ThreadTS, "title": flags.Title,
		}
	case clientapi.ConnectionTypeDiscord:
		manifest.Spec["discord"] = map[string]any{"webhookID": flags.WebhookID, "token": flags.Token}
	case clientapi.ConnectionTypeEmail:
		manifest.Spec["smtp"] = map[string]any{
			"host": flags.Host, "username": envVar(flags.Username), "password": envVar(flags.Password),
			"port": flags.Port, "fromAddress": flags.FromAddress, "fromName": flags.FromName,
			"subject": flags.Subject, "auth": flags.Auth, "insecureTLS": flags.InsecureTLS,
		}
	case clientapi.ConnectionTypeTelegram:
		manifest.Spec["telegram"] = map[string]any{"token": envVar(flags.Token), "chats": envVar(flags.Chats)}
	case clientapi.ConnectionTypeNtfy:
		manifest.Spec["ntfy"] = map[string]any{
			"host": flags.Host, "topic": flags.Topic, "username": envVar(flags.Username), "password": envVar(flags.Password),
		}
	case clientapi.ConnectionTypePushbullet:
		manifest.Spec["pushbullet"] = map[string]any{"token": envVar(flags.Token), "targets": flags.Targets}
	case clientapi.ConnectionTypePushover:
		manifest.Spec["pushover"] = map[string]any{"token": envVar(flags.Token), "user": flags.User}
	case clientapi.ConnectionTypeHTTP:
		manifest.Spec["http"] = map[string]any{
			"url": flags.URL, "insecureTLS": flags.InsecureTLS, "username": envVarPtr(flags.Username),
			"password": envVarPtr(flags.Password), "bearer": envVar(flags.Bearer),
		}
	case clientapi.ConnectionTypeGit:
		manifest.Spec["git"] = map[string]any{
			"url": flags.URL, "ref": flags.Ref, "certificate": envVarPtr(flags.Certificate),
			"username": envVarPtr(flags.Username), "password": envVarPtr(flags.Password),
		}
	case clientapi.ConnectionTypeGithub:
		manifest.Spec["github"] = map[string]any{"url": flags.URL, "personalAccessToken": envVar(flags.PersonalAccessToken)}
	case clientapi.ConnectionTypeGitlab:
		manifest.Spec["gitlab"] = map[string]any{"url": flags.URL, "personalAccessToken": envVar(flags.PersonalAccessToken)}
	case clientapi.ConnectionTypeKubernetes:
		manifest.Spec["kubernetes"] = map[string]any{"certificate": envVar(flags.Certificate)}
	case clientapi.ConnectionTypeFolder:
		manifest.Spec["folder"] = map[string]any{"path": flags.Path}
	case clientapi.ConnectionTypeSFTP:
		manifest.Spec["sftp"] = map[string]any{
			"host": envVar(flags.Host), "username": envVar(flags.Username), "password": envVar(flags.Password),
			"port": flags.Port, "path": flags.Path,
		}
	case clientapi.ConnectionTypeSMB:
		manifest.Spec["smb"] = map[string]any{
			"server": envVar(flags.Host), "username": envVar(flags.Username), "password": envVar(flags.Password),
		}
	case clientapi.ConnectionTypePrometheus:
		manifest.Spec["prometheus"] = map[string]any{
			"url": envVar(flags.URL), "username": envVar(flags.Username),
			"password": envVar(flags.Password), "bearer": envVar(flags.Bearer),
		}
	case clientapi.ConnectionTypeLoki:
		manifest.Spec["loki"] = map[string]any{
			"url": flags.URL, "username": envVar(flags.Username), "password": envVar(flags.Password),
		}
	case clientapi.ConnectionTypeOpenAI:
		manifest.Spec["openai"] = buildAIModelSpec(flags, true)
	case clientapi.ConnectionTypeAnthropic:
		manifest.Spec["anthropic"] = buildAIModelSpec(flags, true)
	case clientapi.ConnectionTypeOllama:
		manifest.Spec["ollama"] = buildAIModelSpec(flags, true)
	case clientapi.ConnectionTypeGemini:
		manifest.Spec["gemini"] = buildAIModelSpec(flags, false)
	case clientapi.ConnectionTypeElasticSearch:
		manifest.Spec["elasticsearch"] = map[string]any{
			"url": flags.URL, "username": envVar(flags.Username), "password": envVar(flags.Password), "insecureTLS": flags.InsecureTLS,
		}
	case clientapi.ConnectionTypeRedis:
		manifest.Spec["redis"] = map[string]any{
			"url": flags.URL, "username": envVar(flags.Username), "password": envVar(flags.Password),
		}
	}

	return manifest
}

func buildAWSSpec(flags *ConnectionFlags) map[string]any {
	spec := map[string]any{"url": envVar(flags.URL), "region": flags.Region}
	if flags.FromProfile != "" {
		spec["accessKey"] = envVarSecretRef(flags.Name, "AWS_ACCESS_KEY_ID")
		spec["secretKey"] = envVarSecretRef(flags.Name, "AWS_SECRET_ACCESS_KEY")
		if flags.SessionToken != "" {
			spec["sessionToken"] = envVarSecretRef(flags.Name, "AWS_SESSION_TOKEN")
		}
	} else {
		spec["accessKey"] = envVar(flags.AccessKey)
		spec["secretKey"] = envVar(flags.SecretKey)
		spec["profile"] = flags.Profile
	}
	return spec
}

func buildAzureSpec(flags *ConnectionFlags) map[string]any {
	return map[string]any{
		"clientID": envVar(flags.ClientID), "clientSecret": envVar(flags.ClientSecret), "tenantID": envVar(flags.TenantID),
	}
}

func buildGCPSpec(flags *ConnectionFlags) map[string]any {
	return map[string]any{"endpoint": envVar(flags.URL), "certificate": envVar(flags.Certificate)}
}

func buildDatabaseSpec(flags *ConnectionFlags, insecureTLS bool) map[string]any {
	spec := map[string]any{
		"url": envVar(flags.URL), "host": envVar(flags.Host), "username": envVar(flags.Username),
		"password": envVar(flags.Password), "database": envVar(flags.Database),
	}
	if insecureTLS {
		spec["insecureTLS"] = flags.InsecureTLS
	}
	return spec
}

func buildAIModelSpec(flags *ConnectionFlags, includeURL bool) map[string]any {
	spec := map[string]any{"apiKey": envVar(flags.ApiKey), "model": flags.Model}
	if includeURL {
		spec["url"] = envVar(flags.URL)
	}
	return spec
}

func marshalConnectionCRD(manifest connectionManifest) ([]byte, error) {
	return marshalManifest(manifest)
}

func buildSecret(flags *ConnectionFlags) secretManifest {
	data := map[string]string{
		"AWS_ACCESS_KEY_ID":     flags.AccessKey,
		"AWS_SECRET_ACCESS_KEY": flags.SecretKey,
	}
	if flags.SessionToken != "" {
		data["AWS_SESSION_TOKEN"] = flags.SessionToken
	}
	return secretManifest{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata:   manifestMetadata{Name: flags.Name, Namespace: flags.Namespace},
		StringData: data,
	}
}

func marshalManifest(manifest any) ([]byte, error) {
	jsonBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, err
	}
	cleanEmptyFields(raw)
	return yaml.Marshal(raw)
}

func marshalDryRunOutput(flags *ConnectionFlags) ([]byte, error) {
	connectionYAML, err := marshalConnectionCRD(buildConnectionCRD(flags))
	if err != nil {
		return nil, fmt.Errorf("marshaling connection: %w", err)
	}
	if flags.FromProfile == "" {
		return connectionYAML, nil
	}

	secretYAML, err := marshalManifest(buildSecret(flags))
	if err != nil {
		return nil, fmt.Errorf("marshaling secret YAML: %w", err)
	}
	return append(secretYAML, append([]byte("---\n"), connectionYAML...)...), nil
}

func cleanEmptyFields(values map[string]any) {
	for key, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			cleanEmptyFields(typed)
			if len(typed) == 0 {
				delete(values, key)
			}
		case string:
			if typed == "" {
				delete(values, key)
			}
		case bool:
			if !typed {
				delete(values, key)
			}
		case float64:
			if typed == 0 {
				delete(values, key)
			}
		case nil:
			delete(values, key)
		case []any:
			if len(typed) == 0 {
				delete(values, key)
			}
		}
	}
}
