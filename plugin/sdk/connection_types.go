package sdk

import "github.com/flanksource/duty/models"

type ConnectionType string

const (
	ConnectionTypeSQL            ConnectionType = "sql"
	ConnectionTypeAnthropic      ConnectionType = models.ConnectionTypeAnthropic
	ConnectionTypeAWS            ConnectionType = models.ConnectionTypeAWS
	ConnectionTypeAWSKMS         ConnectionType = models.ConnectionTypeAWSKMS
	ConnectionTypeAzure          ConnectionType = models.ConnectionTypeAzure
	ConnectionTypeAzureDevops    ConnectionType = models.ConnectionTypeAzureDevops
	ConnectionTypeAzureKeyVault  ConnectionType = models.ConnectionTypeAzureKeyVault
	ConnectionTypeDiscord        ConnectionType = models.ConnectionTypeDiscord
	ConnectionTypeDynatrace      ConnectionType = models.ConnectionTypeDynatrace
	ConnectionTypeElasticSearch  ConnectionType = models.ConnectionTypeElasticSearch
	ConnectionTypeEmail          ConnectionType = models.ConnectionTypeEmail
	ConnectionTypeFacet          ConnectionType = models.ConnectionTypeFacet
	ConnectionTypeFolder         ConnectionType = models.ConnectionTypeFolder
	ConnectionTypeGCP            ConnectionType = models.ConnectionTypeGCP
	ConnectionTypeGCPKMS         ConnectionType = models.ConnectionTypeGCPKMS
	ConnectionTypeGCS            ConnectionType = models.ConnectionTypeGCS
	ConnectionTypeGemini         ConnectionType = models.ConnectionTypeGemini
	ConnectionTypeGenericWebhook ConnectionType = models.ConnectionTypeGenericWebhook
	ConnectionTypeGit            ConnectionType = models.ConnectionTypeGit
	ConnectionTypeGithub         ConnectionType = models.ConnectionTypeGithub
	ConnectionTypeGitlab         ConnectionType = models.ConnectionTypeGitlab
	ConnectionTypeGoogleChat     ConnectionType = models.ConnectionTypeGoogleChat
	ConnectionTypeHTTP           ConnectionType = models.ConnectionTypeHTTP
	ConnectionTypeIFTTT          ConnectionType = models.ConnectionTypeIFTTT
	ConnectionTypeJMeter         ConnectionType = models.ConnectionTypeJMeter
	ConnectionTypeKubernetes     ConnectionType = models.ConnectionTypeKubernetes
	ConnectionTypeLDAP           ConnectionType = models.ConnectionTypeLDAP
	ConnectionTypeLoki           ConnectionType = models.ConnectionTypeLoki
	ConnectionTypeMatrix         ConnectionType = models.ConnectionTypeMatrix
	ConnectionTypeMattermost     ConnectionType = models.ConnectionTypeMattermost
	ConnectionTypeMongo          ConnectionType = models.ConnectionTypeMongo
	ConnectionTypeMySQL          ConnectionType = models.ConnectionTypeMySQL
	ConnectionTypeNtfy           ConnectionType = models.ConnectionTypeNtfy
	ConnectionTypeOllama         ConnectionType = models.ConnectionTypeOllama
	ConnectionTypeOpenAI         ConnectionType = models.ConnectionTypeOpenAI
	ConnectionTypeOpenSearch     ConnectionType = models.ConnectionTypeOpenSearch
	ConnectionTypeOpsGenie       ConnectionType = models.ConnectionTypeOpsGenie
	ConnectionTypePostgres       ConnectionType = models.ConnectionTypePostgres
	ConnectionTypePrometheus     ConnectionType = models.ConnectionTypePrometheus
	ConnectionTypePushbullet     ConnectionType = models.ConnectionTypePushbullet
	ConnectionTypePushover       ConnectionType = models.ConnectionTypePushover
	ConnectionTypeRedis          ConnectionType = models.ConnectionTypeRedis
	ConnectionTypeRestic         ConnectionType = models.ConnectionTypeRestic
	ConnectionTypeRocketchat     ConnectionType = models.ConnectionTypeRocketchat
	ConnectionTypeS3             ConnectionType = models.ConnectionTypeS3
	ConnectionTypeSFTP           ConnectionType = models.ConnectionTypeSFTP
	ConnectionTypeSlack          ConnectionType = models.ConnectionTypeSlack
	ConnectionTypeSlackWebhook   ConnectionType = models.ConnectionTypeSlackWebhook
	ConnectionTypeSMB            ConnectionType = models.ConnectionTypeSMB
	ConnectionTypeSQLServer      ConnectionType = models.ConnectionTypeSQLServer
	ConnectionTypeTeams          ConnectionType = models.ConnectionTypeTeams
	ConnectionTypeTelegram       ConnectionType = models.ConnectionTypeTelegram
	ConnectionTypeWebhook        ConnectionType = models.ConnectionTypeWebhook
	ConnectionTypeWindows        ConnectionType = models.ConnectionTypeWindows
	ConnectionTypeZulipChat      ConnectionType = models.ConnectionTypeZulipChat
)
