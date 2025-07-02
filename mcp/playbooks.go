package mcp

import (
	gocontext "context"
	"encoding/json"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func playbookRecentRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	pbrs, err := db.GetRecentPlaybookRuns(ctx, limit)
	jsonData, err := json.Marshal(pbrs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil

}

func playbookFailedRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	limit := req.GetInt("limit", 20)

	runs, err := db.GetRecentPlaybookRuns(ctx, limit, models.PlaybookRunStatusFailed)
	jsonData, err := json.Marshal(runs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil

}

func playbookRunHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	playbookID := req.GetString("id", "")
	params := req.GetArguments()["params"]

	pb, err := query.FindPlaybook(ctx, playbookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if pb == nil {
		return mcp.NewToolResultError("playbook[%s] not found"), nil
	}

	pj, err := json.Marshal(params)

	var rp playbook.RunParams
	if err := json.Unmarshal(pj, &rp); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	run, err := playbook.Run(ctx, pb, rp)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	jsonData, err := json.Marshal(run)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil
}

func playbookListResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	type playbookParams struct {
		Name string
		Type string
	}

	type playbookWithParams struct {
		ID        uuid.UUID
		Name      string
		Namespace string
		Params    []playbookParams
	}

	var pbs []models.Playbook
	err = ctx.DB().Where("deleted_at IS NULL").Find(&pbs).Error
	if err != nil {
		return nil, err
	}

	var resources []mcp.ResourceContents
	for _, pb := range pbs {
		var parsedSpec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &parsedSpec); err != nil {
			return nil, err
		}
		var params []playbookParams

		for _, param := range parsedSpec.Parameters {
			params = append(params, playbookParams{
				Name: param.Name,
				Type: string(param.Type),
			})
		}

		p := playbookWithParams{
			ID:        pb.ID,
			Name:      pb.Name,
			Namespace: pb.Namespace,
			Params:    params,
		}

		jsonData, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}

		resources = append(resources, mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: echo.MIMEApplicationJSON,
			Text:     string(jsonData),
		})

	}

	return resources, nil
}

func registerPlaybook(s *server.MCPServer) {
	s.AddResource(mcp.NewResource("playbooks://all", "All Playbooks",
		mcp.WithResourceDescription("List all available playbooks"), mcp.WithMIMEType(echo.MIMEApplicationJSON)),
		playbookListResourceHandler)

	playbookRecentRunTool := mcp.NewTool("playbook_recent_runs",
		mcp.WithDescription("Playbook recent runs"),
		mcp.WithNumber("limit",
			mcp.Required(),
			mcp.Description("Search query"),
		))

	s.AddTool(playbookRecentRunTool, playbookRecentRunHandler)

	playbookFailedRunTool := mcp.NewTool("playbook_failed_runs",
		mcp.WithDescription("Playbook recent runs"),
		mcp.WithNumber("limit",
			mcp.Required(),
			mcp.Description("Search query"),
		))

	s.AddTool(playbookFailedRunTool, playbookFailedRunHandler)

	playbookRunTool := mcp.NewTool("playbook_exec_run",
		mcp.WithDescription("Playbook execute run."),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Playbook ID"),
		),
		mcp.WithObject("params", mcp.Required(), mcp.Description("Params for the playbook. Each playbook has its own parameters which can be found in ListPlaybooks resource")))

	s.AddTool(playbookRunTool, playbookRunHandler)
}

func extractID(uri string) string {
	// Extract ID from "users://123" format
	parts := strings.Split(uri, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
