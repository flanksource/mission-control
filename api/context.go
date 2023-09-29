package api

import (
	gocontext "context"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

var DefaultContext Context

// ContextUser carries basic information of the current logged in user
type ContextUser struct {
	ID    uuid.UUID
	Email string
}

// type alias because the name "Context" collides with gocontext
// and embedding both wouldn't have been possible.
type EchoContext = echo.Context

type Context interface {
	EchoContext
	duty.DBContext

	Namespace() string
	Kubernetes() kubernetes.Interface

	WithDB(db *gorm.DB) Context
	WithEchoContext(ctx EchoContext) Context
	WithContext(ctx gocontext.Context) Context

	WithUser(user *ContextUser) Context
	User() *ContextUser

	StartTrace(tracerName string, spanName string) (Context, trace.Span)
	SetSpanAttributes(attrs ...attribute.KeyValue)

	GetEnvVarValue(input types.EnvVar) (string, error)
	GetEnvValueFromCache(env types.EnvVar) (string, error)

	HydrateConnection(connectionIdentifier string) (*models.Connection, error)
}

// context implements Context
type context struct {
	EchoContext
	gocontext.Context

	user *ContextUser

	db   *gorm.DB
	pool *pgxpool.Pool

	kubernetes kubernetes.Interface
	namespace  string
}

func NewContext(db *gorm.DB, pool *pgxpool.Pool) Context {
	c := &context{
		Context:    gocontext.Background(),
		db:         db,
		pool:       pool,
		kubernetes: Kubernetes,
		namespace:  Namespace,
	}

	return c
}

func (c *context) Kubernetes() kubernetes.Interface {
	return c.kubernetes
}

func (c *context) Namespace() string {
	return c.namespace
}

func (c context) WithEchoContext(ctx EchoContext) Context {
	c.EchoContext = ctx
	c.Context = c.Request().Context()
	return &c
}

func (c context) WithContext(ctx gocontext.Context) Context {
	c.Context = ctx
	return &c
}

func (c context) StartTrace(tracerName, spanName string) (Context, trace.Span) {
	tracer := otel.GetTracerProvider().Tracer(tracerName)
	traceCtx, span := tracer.Start(c.Context, spanName)
	c.Context = traceCtx
	return &c, span
}

func (c *context) SetSpanAttributes(attrs ...attribute.KeyValue) {
	trace.SpanFromContext(c).SetAttributes(attrs...)
}

func (c context) WithDB(db *gorm.DB) Context {
	c.db = db
	return &c
}

func (c *context) DB() *gorm.DB {
	if c.db == nil {
		return nil
	}

	return c.db.WithContext(c.Context)
}

func (c *context) Pool() *pgxpool.Pool {
	if c.pool == nil {
		return nil
	}

	return c.pool
}

func (c context) WithUser(user *ContextUser) Context {
	c.user = user
	c.SetSpanAttributes(attribute.String("user-id", user.ID.String()))
	return &c
}

func (c *context) User() *ContextUser {
	return c.user
}

func (c *context) GetEnvVarValue(input types.EnvVar) (string, error) {
	return duty.GetEnvValueFromCache(c.kubernetes, input, c.namespace)
}

func (ctx *context) GetEnvValueFromCache(env types.EnvVar) (string, error) {
	return duty.GetEnvValueFromCache(ctx.kubernetes, env, ctx.namespace)
}

// HydrateConnection finds the connection by the given identifier & hydrates it.
// connectionIdentifier can either be the connection id or the full connection name.
func (c *context) HydrateConnection(connectionIdentifier string) (*models.Connection, error) {
	if connectionIdentifier == "" {
		return nil, nil
	}

	if c.DB() == nil {
		return nil, Errorf(EINTERNAL, "DB has not been initialized")
	}

	connection, err := duty.HydratedConnectionByURL(c, c.DB(), c.kubernetes, c.namespace, connectionIdentifier)
	if err != nil {
		return nil, err
	}

	// Connection name was explicitly provided but was not found.
	// That's an error.
	if connection == nil {
		return nil, Errorf(ENOTFOUND, "connection %q not found", connectionIdentifier)
	}

	return connection, nil
}
