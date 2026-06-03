package sdk

type ConnectionType string

const (
	ConnectionTypeKubernetes ConnectionType = "kubernetes"
	ConnectionTypeMySQL      ConnectionType = "mysql"
	ConnectionTypeSQLServer  ConnectionType = "sql_server"
)
