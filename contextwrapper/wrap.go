package contextwrapper

import (
	gocontext "context"
	commonsCtx "github.com/flanksource/commons/context"
	"github.com/flanksource/duty/context"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

func ContextWrapper(gormDB *gorm.DB, pool *pgxpool.Pool, k8s kubernetes.Interface, tracer trace.Tracer) func(gocontext.Context) context.Context {
	return func(ctx gocontext.Context) context.Context {
		c := context.NewContext(ctx, commonsCtx.WithTracer(tracer))
		c = c.WithDB(gormDB, pool).WithKubernetes(k8s)
		return c
	}
}
