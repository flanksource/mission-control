package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gorm.io/gorm"
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
	pb, err := query.FindPlaybook(ctx, playbookID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if pb == nil {
		return mcp.NewToolResultError(fmt.Sprintf("playbook[%s] not found", playbookID)), nil
	}

	params := req.GetArguments()
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

func addPlaybooksAsTools(ctx context.Context, s *server.MCPServer) error {
	playbooks, err := gorm.G[models.Playbook](ctx.DB()).Where("deleted_at IS NULL").Find(ctx)
	if err != nil {
		return fmt.Errorf("error fetching playbooks: %w", err)
	}
	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			return fmt.Errorf("error unmarshaling playbook[%s] spec: %w", pb.ID, err)
		}

		root := &jsonschema.Schema{
			Type:     "object",
			Required: []string{"id"},
		}

		root.Properties.Set("id", &jsonschema.Schema{Type: "string", Description: "UUID of playbook to execute"})
		root.Properties.Set("agent_id", &jsonschema.Schema{Type: "string", Description: "UUID of agent to run playbook on. Leave empty if not specified"})
		if len(spec.Checks) > 0 {
			root.Properties.Set("check_id", &jsonschema.Schema{Type: "string", Description: "UUID of the health check this playbook belongs to"})
		}
		if len(spec.Configs) > 0 {
			root.Properties.Set("config_id", &jsonschema.Schema{Type: "string", Description: "UUID of config_item/catalog_item this playbook belongs to"})
		}
		if len(spec.Components) > 0 {
			root.Properties.Set("component_id", &jsonschema.Schema{Type: "string", Description: "UUID of component this playbook belongs to"})
		}

		paramsSchema := &jsonschema.Schema{Type: "object"}
		var requiredParams []string
		for _, param := range spec.Parameters {
			s := &jsonschema.Schema{Type: "string", Description: param.Description}
			// RunParams only has support for strings so we use enum in these cases
			if param.Type == v1.PlaybookParameterTypeCheckbox {
				s.Enum = []any{"true", "false"}
				s.Default = "false"
				s.Description += ". This is a boolean field, either true or false as strings."
			}
			paramsSchema.Properties.Set(param.Name, s)
			if param.Required {
				requiredParams = append(requiredParams, param.Name)
			}
		}
		paramsSchema.Required = requiredParams
		root.Properties.Set("params", paramsSchema)

		rj, err := root.MarshalJSON()
		if err != nil {
			return fmt.Errorf("error marshalling root json schema: %w", err)
		}

		s.AddTool(mcp.NewToolWithRawSchema("playbook_exec_"+pb.ID.String(), "Run the playbook: "+pb.Name+"\n"+pb.Description, rj), playbookRunHandler)
	}
	return nil
}

func registerPlaybook(ctx context.Context, s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("playbook://{id}", "Playbook",
			mcp.WithTemplateDescription("Playbook data"), mcp.WithTemplateMIMEType(echo.MIMEApplicationJSON)),
		playbookResourceHandler,
	)

	s.AddTool(mcp.NewTool("playbooks_list_all",
		mcp.WithDescription(`
			List all available playbooks. These playbooks can be executed by calling the tool playbook_exec_<uuid>.
			If a playbook's uuid is 7f373ce2-e064-478a-b31e-33407a92ae0b, if the user asks to execute the playbook,
			call the tool playbook_exec_7f373ce2-e064-478a-b31e-33407a92ae0b with its input.
			ALWAYS CONFIRM WITH USER BEFORE CALLING THE playbook_exec_<uuid> TOOL BY SHOWING ALL THE TOOL NAME AND COMPLETE INPUT TO BE PASSED.
		`)), playbookListToolHandler)

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

	addPlaybooksAsTools(ctx, s)
}

func extractID(uri string) string {
	// Extract ID from "users://123" format
	parts := strings.Split(uri, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
