package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
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
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

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
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
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
		return mcp.NewToolResultError(fmt.Sprintf("playbook[%s] not found", playbookID)), nil
	}

	pj, err := json.Marshal(params)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

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

func toPlaybookWithParams(pb models.Playbook) (playbookWithParams, error) {
	var parsedSpec v1.PlaybookSpec
	if err := json.Unmarshal(pb.Spec, &parsedSpec); err != nil {
		return playbookWithParams{}, err
	}
	var params []playbookParams
	for _, param := range parsedSpec.Parameters {
		params = append(params, playbookParams{
			Name: param.Name,
			Type: string(param.Type),
		})
	}

	return playbookWithParams{
		ID:        pb.ID,
		Name:      pb.Name,
		Namespace: pb.Namespace,
		Params:    params,
	}, nil
}

func playbookListToolHandler(goctx gocontext.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	var pbs []models.Playbook
	err = ctx.DB().Where("deleted_at IS NULL").Find(&pbs).Error
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var allPlaybooks []playbookWithParams
	for _, pb := range pbs {
		pbWithParams, err := toPlaybookWithParams(pb)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		allPlaybooks = append(allPlaybooks, pbWithParams)
	}

	jsonData, err := json.Marshal(allPlaybooks)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(jsonData)), nil
}

func playbookResourceHandler(goctx gocontext.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return nil, err
	}

	id := extractID(req.Params.URI)
	pb, err := query.FindPlaybook(ctx, id)
	if err != nil {
		return nil, err
	}
	if pb == nil {
		return nil, fmt.Errorf("playbook[%s] not found", id)
	}
	jsonData, err := json.Marshal(pb)
	if err != nil {
		return nil, err
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonData),
		},
	}, nil
}

func registerPlaybook(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("playbook://{id}", "Playbook",
			mcp.WithTemplateDescription("Playbook data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		playbookResourceHandler,
	)

	s.AddTool(mcp.NewTool("playbooks_list_all",
		mcp.WithDescription("List all available playbooks")), playbookListToolHandler)

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
		mcp.WithDestructiveHintAnnotation(true),
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
