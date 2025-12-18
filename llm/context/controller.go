package context

import (
	"net/http"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /catalog routes")

	e.GET("/llm/context/config/:id", GetKnowledgeGraph)
}

func GetKnowledgeGraph(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	configID := c.Param("id")

	var llmContext *Context
	err := auth.WithRLS(ctx, func(txCtx context.Context) error {
		var err error
		llmContext, err = Create(txCtx, defaultContextRequest(configID))
		return err
	})
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, llmContext)
}

func defaultContextRequest(configID string) api.LLMContextRequest {
	return api.LLMContextRequest{
		Config:  configID,
		Changes: &api.TimeMetadata{Since: "4h"},
		Relationships: []api.LLMContextRequestRelationship{
			{Direction: query.All},
		},
	}
}
