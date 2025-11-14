package mcp

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/invopop/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/samber/lo"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
)

// playbookToolNameToID maps tool names to playbook IDs
var (
	playbookToolNameToID = sync.Map{}
)

func addPlaybooksAsTool(goctx gocontext.Context, srv *server.MCPServer, session server.ClientSession) error {
	sessionID := session.SessionID()

	ctx, err := getDutyCtx(goctx)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get duty context for session %s", sessionID)
	}

	var playbooks []models.Playbook
	err = auth.WithRLS(ctx, func(txCtx context.Context) error {
		var err error
		playbooks, err = gorm.G[models.Playbook](txCtx.DB()).Where("deleted_at IS NULL").Find(txCtx)
		return err
	})
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to get playbook tools for session %s", sessionID)
	}

	tools, err := getPlaybooksAsTools(playbooks)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to convert playbooks to tools for session %s", sessionID)
	}

	sessionTools := lo.Map(tools, func(tool mcp.Tool, _ int) server.ServerTool {
		return server.ServerTool{
			Tool:    tool,
			Handler: playbookRunHandler,
		}
	})
	if err := srv.AddSessionTools(sessionID, sessionTools...); err != nil {
		return ctx.Oops().Wrapf(err, "failed to add tool %d to session %s", len(sessionTools), sessionID)
	}

	ctx.Logger.Infof("Successfully registered %d playbook tools for session %q", len(tools), sessionID)
	return nil
}

func getPlaybooksAsTools(playbooks []models.Playbook) ([]mcp.Tool, error) {
	var newPlaybookTools []mcp.Tool
	for _, pb := range playbooks {
		var spec v1.PlaybookSpec
		if err := json.Unmarshal(pb.Spec, &spec); err != nil {
			return nil, fmt.Errorf("error unmarshaling playbook[%s] spec: %w", pb.ID, err)
		}

		root := &jsonschema.Schema{
			Type:       "object",
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}

		root.Properties.Set("agent_id", &jsonschema.Schema{Type: "string", Description: "UUID of agent to run playbook on. Leave empty if not specified"})
		if len(spec.Checks) > 0 {
			root.Properties.Set("check_id", &jsonschema.Schema{Type: "string", Description: "UUID of the health check this playbook belongs to"})
			root.Required = append(root.Required, "check_id")
		}
		if len(spec.Configs) > 0 {
			root.Properties.Set("config_id", &jsonschema.Schema{Type: "string", Description: "UUID of config item (from search_catalog results). Example: f47ac10b-58cc-4372-a567-0e02b2c3d479"})
			root.Required = append(root.Required, "config_id")
		}
		if len(spec.Components) > 0 {
			root.Properties.Set("component_id", &jsonschema.Schema{Type: "string", Description: "UUID of component this playbook belongs to"})
			root.Required = append(root.Required, "component_id")
		}

		paramsSchema := &jsonschema.Schema{
			Type:       "object",
			Properties: orderedmap.New[string, *jsonschema.Schema](),
		}
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
			return nil, fmt.Errorf("error marshaling root json schema: %w", err)
		}

		// Generate descriptive tool name and store mapping
		toolName := generatePlaybookToolName(pb)
		playbookToolNameToID.Store(toolName, pb.ID.String())

		t := mcp.NewToolWithRawSchema(toolName, pb.Description, rj)
		t.Annotations.Title = pb.Name
		newPlaybookTools = append(newPlaybookTools, t)
	}

	return newPlaybookTools, nil
}
