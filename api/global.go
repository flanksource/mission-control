package api

import (
	gocontext "context"
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

var SystemUserID *uuid.UUID
var CanaryCheckerPath string
var ApmHubPath string
var Kubernetes kubernetes.Interface
var Namespace string

type Context struct {
	gocontext.Context
	DB         *gorm.DB
	Kubernetes kubernetes.Interface
	Namespace  string
}

func (c *Context) GetEnvVarValue(input types.EnvVar) (string, error) {
	return duty.GetEnvValueFromCache(c.Kubernetes, input, c.Namespace)
}

func NewContext(db *gorm.DB) *Context {
	return &Context{
		Context:    gocontext.Background(),
		DB:         db,
		Kubernetes: Kubernetes,
		Namespace:  Namespace,
	}
}

func (c *Context) HydrateConnection(connectionName string) (*models.Connection, error) {
	if connectionName == "" || !strings.HasPrefix(connectionName, "connection://") {
		return nil, nil
	}

	if c.DB == nil {
		return nil, errors.New("DB has not been initialized")
	}

	connection, err := duty.HydratedConnectionByURL(c, c.DB, c.Kubernetes, c.Namespace, connectionName)
	if err != nil {
		return nil, err
	}

	// Connection name was explicitly provided but was not found.
	// That's an error.
	if connection == nil {
		return nil, fmt.Errorf("connection %q not found", connectionName)
	}

	return connection, nil
}
