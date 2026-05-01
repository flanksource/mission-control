package catalog

import (
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac/policy"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	logger.Infof("Registering /catalog routes")

	apiGroup := e.Group("/catalog", rbac.Catalog(policy.ActionRead))
	apiGroup.POST("/summary", SearchConfigSummary, echoSrv.RLSMiddleware)

	apiGroup.POST("/changes", SearchCatalogChanges, echoSrv.RLSMiddleware)
	// Deprecated. Use POST
	apiGroup.GET("/changes", SearchCatalogChanges, echoSrv.RLSMiddleware)
	apiGroup.POST("/report/preview", PreviewCatalogReport, echoSrv.RLSMiddleware)
	apiGroup.POST("/report", GenerateCatalogReport, echoSrv.RLSMiddleware)
	apiGroup.GET("/:id/relationships", GetConfigRelationships, echoSrv.RLSMiddleware)

	deleteGroup := e.Group("/catalog", rbac.Catalog(policy.ActionDelete))
	deleteGroup.POST("/config-items/bulk-delete", BulkDeleteConfigItems, echoSrv.RLSMiddleware)
}

type ConfigRelationshipsResponse struct {
	ID       uuid.UUID             `json:"id"`
	Incoming *query.ConfigTreeNode `json:"incoming"`
	Outgoing *query.ConfigTreeNode `json:"outgoing"`
}

type BulkDeleteConfigItemsRequest struct {
	IDs []string `json:"ids"`
}

type BulkDeleteConfigItemsResponse struct {
	Deleted int      `json:"deleted"`
	IDs     []string `json:"ids"`
}

func BulkDeleteConfigItems(c echo.Context) error {
	var req BulkDeleteConfigItemsRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ids, err := normalizeConfigItemIDs(req.IDs)
	if err != nil {
		return api.WriteError(c, err)
	}

	ctx := c.Request().Context().(context.Context)
	if err := ctx.DB().Exec("select drop_config_items(?)", pq.StringArray(ids)).Error; err != nil {
		return api.WriteError(c, ctx.Oops().Wrapf(err, "delete config items"))
	}

	return c.JSON(http.StatusOK, BulkDeleteConfigItemsResponse{
		Deleted: len(ids),
		IDs:     ids,
	})
}

func normalizeConfigItemIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, api.Errorf(api.EINVALID, "ids is required")
	}

	ids := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		raw := strings.TrimSpace(value)
		if raw == "" {
			return nil, api.Errorf(api.EINVALID, "config item id cannot be empty")
		}

		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "invalid config item id: %s", value)
		}

		normalized := id.String()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		ids = append(ids, normalized)
	}

	if len(ids) == 0 {
		return nil, api.Errorf(api.EINVALID, "ids is required")
	}

	return ids, nil
}

func SearchCatalogChanges(c echo.Context) error {
	var req query.CatalogChangesSearchRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ctx := c.Request().Context().(context.Context)

	response, err := query.FindCatalogChanges(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func SearchConfigSummary(c echo.Context) error {
	var req query.ConfigSummaryRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request: %v", err))
	}

	ctx := c.Request().Context().(context.Context)

	response, err := query.ConfigSummary(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func GetConfigRelationships(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid config id: %s", c.Param("id")))
	}

	ctx := c.Request().Context().(context.Context)

	incoming, err := configRelationshipTree(ctx, id, query.Incoming)
	if err != nil {
		return api.WriteError(c, err)
	}
	outgoing, err := configRelationshipTree(ctx, id, query.Outgoing)
	if err != nil {
		return api.WriteError(c, err)
	}

	if incoming == nil && outgoing == nil {
		return api.WriteError(c, api.Errorf(api.ENOTFOUND, "config %s not found", id))
	}

	return c.JSON(http.StatusOK, ConfigRelationshipsResponse{
		ID:       id,
		Incoming: incoming,
		Outgoing: outgoing,
	})
}

func configRelationshipTree(ctx context.Context, id uuid.UUID, direction query.RelationDirection) (*query.ConfigTreeNode, error) {
	tree, err := query.ConfigTree(ctx, id, query.ConfigTreeOptions{
		Direction: direction,
		Incoming:  query.Both,
		Outgoing:  query.Both,
	})
	if err != nil || tree == nil {
		return tree, err
	}
	return findConfigTreeNode(tree, id), nil
}

func findConfigTreeNode(node *query.ConfigTreeNode, id uuid.UUID) *query.ConfigTreeNode {
	if node == nil {
		return nil
	}
	if node.ID == id {
		return node
	}
	for _, child := range node.Children {
		if found := findConfigTreeNode(child, id); found != nil {
			return found
		}
	}
	return nil
}
