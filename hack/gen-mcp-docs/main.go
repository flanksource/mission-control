package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/flanksource/incident-commander/mcp"
)

func main() {
	s := server.NewMCPServer("mission-control", "dev",
		server.WithToolCapabilities(true),
	)
	mcp.RegisterStaticTools(s)

	tools := s.ListTools()
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("# MCP Server Reference")
	fmt.Println()

	printResourceTemplates()
	printPrompts()
	printToolSummary(names, tools)
	printDynamicTools()

	fmt.Println("## Tool Details")
	fmt.Println()

	for _, name := range names {
		printTool(tools[name].Tool)
	}
}

func printResourceTemplates() {
	fmt.Println("## Resource Templates")
	fmt.Println()
	fmt.Println("| URI Template | Description |")
	fmt.Println("|-------------|-------------|")
	fmt.Println("| `config_item://{id}` | Returns the complete JSON representation of an infrastructure configuration item (AWS EC2 instance, Kubernetes deployment, etc). Use to read the full state of a known item; use `search_catalog` and `describe_catalog` to discover resources. Example: `config_item://i-0abcd1234efgh5678` |")
	fmt.Println("| `playbook://{idOrName}` | Returns the JSON definition of an automated runbook including steps and parameters. Use to inspect a playbook's logic; use the dynamic per-session playbook tools to execute it. Example: `playbook://restart-k8s-pods` |")
	fmt.Println("| `connection://{namespace}/{name}` | Returns JSON configuration for an external service endpoint (database, API, cloud provider). Use to inspect a known connection; use `list_connections` to discover available connections. Example: `connection://default/postgres-main` |")
	fmt.Println("| `view://{namespace}/{name}` | Returns the JSON structural definition and query logic of a saved view/dashboard. Use to understand how a view is constructed; use the dynamic view tools to execute the query and fetch data. Example: `view://monitoring/high-cpu-instances` |")
	fmt.Println()
}

func printPrompts() {
	fmt.Println("## Prompts")
	fmt.Println()
	fmt.Println("| Name | Description |")
	fmt.Println("|------|-------------|")
	fmt.Println("| `Unhealthy catalog items` | Searches for all unhealthy items using `search_catalog` with query `health!=healthy` |")
	fmt.Println("| `troubleshoot_kubernetes_resource` | Troubleshoots Kubernetes resources. Accepts optional `query` argument (default: `health!=healthy type=Kubernetes::*`) |")
	fmt.Println()
}

func printToolSummary(names []string, tools map[string]*server.ServerTool) {
	fmt.Printf("## Tools (%d static + dynamic)\n\n", len(names))
	fmt.Println("| Tool | Hints | Description |")
	fmt.Println("|------|-------|-------------|")
	for _, name := range names {
		tool := tools[name].Tool
		hints := formatHints(tool.Annotations)
		desc := firstSentence(tool.Description)
		fmt.Printf("| [`%s`](#%s) | %s | %s |\n", name, name, hints, desc)
	}
	fmt.Println("| `{playbook}_{namespace}_{category}` | mutating | Dynamic per-session playbook tools. Parameters derived from playbook spec. |")
	fmt.Println("| `view_{name}_{namespace}` | read-only | Dynamic view tools synced hourly. Returns table rows by default with select/page/limit controls. |")
	fmt.Println()
}

func printDynamicTools() {
	fmt.Println("### Dynamic Tools")
	fmt.Println()
	fmt.Println("These tools are registered at runtime and do not appear in the static list above.")
	fmt.Println()
	fmt.Println("**Playbook Tools** — Each playbook is registered as an MCP tool per-session.")
	fmt.Println("Names follow the pattern `{name}_{namespace}_{category}` (e.g. `restart-deployment_mission-control_kubernetes`).")
	fmt.Println("Parameters are derived from each playbook's parameter spec. These tools are **mutating**.")
	fmt.Println()
	fmt.Println("**View Tools** — Each view is registered as an MCP tool, synced every hour.")
	fmt.Println("Names follow the pattern `view_{name}_{namespace}` (e.g. `view_pod-overview_mission-control`).")
	fmt.Println("All view tools are **read-only** and accept these common parameters:")
	fmt.Println()
	fmt.Println("| Name | Type | Default | Description |")
	fmt.Println("|------|------|---------|-------------|")
	fmt.Println("| `withRows` | boolean | true | Include table rows (paginated) |")
	fmt.Println("| `withPanels` | boolean | false | Include panel data |")
	fmt.Println("| `select` | array | all | Columns to include in result |")
	fmt.Println("| `page` | integer | 1 | Page number (1-based) |")
	fmt.Println("| `limit` | integer | 50 | Rows per page (max 500) |")
	fmt.Println()
	fmt.Println("Views may also expose template variables as additional string parameters.")
	fmt.Println()
}

func printTool(tool mcplib.Tool) {
	fmt.Printf("### `%s`\n\n", tool.Name)

	if tool.Description != "" {
		fmt.Printf("%s\n\n", strings.TrimSpace(tool.Description))
	}

	fmt.Printf("**Hints:** %s\n\n", formatHints(tool.Annotations))
	printInputSchema(tool)
}

func formatHints(a mcplib.ToolAnnotation) string {
	readOnly := a.ReadOnlyHint != nil && *a.ReadOnlyHint
	destructive := a.DestructiveHint != nil && *a.DestructiveHint
	idempotent := a.IdempotentHint != nil && *a.IdempotentHint

	var hints []string
	if readOnly {
		hints = append(hints, "read-only")
	} else if destructive {
		hints = append(hints, "destructive")
	} else {
		hints = append(hints, "mutating")
	}
	if idempotent {
		hints = append(hints, "idempotent")
	}
	return strings.Join(hints, ", ")
}

func printInputSchema(tool mcplib.Tool) {
	props := tool.InputSchema.Properties
	if tool.RawInputSchema != nil {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(tool.RawInputSchema, &raw); err == nil {
			if p, ok := raw["properties"]; ok {
				var parsed map[string]json.RawMessage
				if err := json.Unmarshal(p, &parsed); err == nil && len(parsed) > 0 {
					printParamsTable(parsed, raw)
					return
				}
			}
		}
	}

	if len(props) == 0 {
		return
	}

	fmt.Println("**Parameters:**")
	fmt.Println()
	fmt.Println("| Name | Type | Required | Description |")
	fmt.Println("|------|------|----------|-------------|")

	reqSet := make(map[string]bool)
	for _, r := range tool.InputSchema.Required {
		reqSet[r] = true
	}

	paramNames := sortedKeys(props)
	for _, name := range paramNames {
		propMap, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		typ := fmt.Sprint(propMap["type"])
		desc := propDesc(propMap)
		req := ""
		if reqSet[name] {
			req = "Yes"
		}
		fmt.Printf("| `%s` | %s | %s | %s |\n", name, typ, req, desc)
	}
	fmt.Println()
}

func printParamsTable(parsed map[string]json.RawMessage, raw map[string]json.RawMessage) {
	fmt.Println("**Parameters:**")
	fmt.Println()
	fmt.Println("| Name | Type | Required | Description |")
	fmt.Println("|------|------|----------|-------------|")

	var required []string
	if r, ok := raw["required"]; ok {
		json.Unmarshal(r, &required)
	}
	reqSet := make(map[string]bool)
	for _, r := range required {
		reqSet[r] = true
	}

	paramNames := make([]string, 0, len(parsed))
	for k := range parsed {
		paramNames = append(paramNames, k)
	}
	sort.Strings(paramNames)

	for _, name := range paramNames {
		var prop map[string]any
		json.Unmarshal(parsed[name], &prop)
		typ := fmt.Sprint(prop["type"])
		desc := propDesc(prop)
		req := ""
		if reqSet[name] {
			req = "Yes"
		}
		fmt.Printf("| `%s` | %s | %s | %s |\n", name, typ, req, desc)
	}
	fmt.Println()
}

func firstSentence(desc string) string {
	desc = strings.TrimSpace(desc)
	desc = strings.Join(strings.Fields(desc), " ")
	for _, sep := range []string{". ", ".\n"} {
		if idx := strings.Index(desc, sep); idx > 0 {
			return desc[:idx+1]
		}
	}
	if strings.HasSuffix(desc, ".") {
		return desc
	}
	return desc
}

func propDesc(prop map[string]any) string {
	desc := fmt.Sprint(prop["description"])
	if desc == "<nil>" {
		return ""
	}
	return firstSentence(desc)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
