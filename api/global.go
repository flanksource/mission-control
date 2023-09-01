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
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

// contextKey represents an internal key for adding context fields.
type contextKey int

// List of context keys.
// These are used to store request-scoped information.
const (
	// stores current logged in user
	userContextKey contextKey = iota
)

// ContextUser carries basic information of the current logged in user
type ContextUser struct {
	ID    uuid.UUID
	Email string
}

const UserIDHeaderKey = "X-User-ID"

var SystemUserID *uuid.UUID
var CanaryCheckerPath string
var ApmHubPath string
var Kubernetes kubernetes.Interface
var Namespace string

// type alias because the name "Context" collides with gocontext
// and embedding both wouldn't have been possible.
type EchoContext = echo.Context

type Context struct {
	EchoContext
	gocontext.Context
	db         *gorm.DB
	Kubernetes kubernetes.Interface
	Namespace  string
}

func NewContext(db *gorm.DB, echoCtx EchoContext) *Context {
	c := &Context{
		Context:     gocontext.Background(),
		EchoContext: echoCtx,
		db:          db,
		Kubernetes:  Kubernetes,
		Namespace:   Namespace,
	}

	if echoCtx != nil {
		c.Context = c.Request().Context()
	}

	return c
}

func (c Context) WithDB(db *gorm.DB) *Context {
	c.db = db
	return &c
}

func (c *Context) DB() *gorm.DB {
	if c.db == nil {
		return nil
	}

	return c.db.WithContext(c.Context)
}

func (c *Context) WithUser(user *ContextUser) {
	c.Context = gocontext.WithValue(c.Context, userContextKey, user)
}

func (c *Context) User() *ContextUser {
	user, ok := c.Context.Value(userContextKey).(*ContextUser)
	if !ok {
		return nil
	}

	return user
}

func (c *Context) GetEnvVarValue(input types.EnvVar) (string, error) {
	return duty.GetEnvValueFromCache(c.Kubernetes, input, c.Namespace)
}

func (ctx *Context) GetEnvValueFromCache(env types.EnvVar) (string, error) {
	return duty.GetEnvValueFromCache(ctx.Kubernetes, env, ctx.Namespace)
}

func (c *Context) HydrateConnection(connectionName string) (*models.Connection, error) {
	if connectionName == "" || !strings.HasPrefix(connectionName, "connection://") {
		return nil, nil
	}

	if c.DB() == nil {
		return nil, errors.New("DB has not been initialized")
	}

	connection, err := duty.HydratedConnectionByURL(c, c.DB(), c.Kubernetes, c.Namespace, connectionName)
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
