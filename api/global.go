package api

import (
	gocontext "context"

	"github.com/flanksource/duty"
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
