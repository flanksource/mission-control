package views

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

func ExportMulti(ctx context.Context, allViews []v1.View, vars map[string]string, format string) ([]byte, error) {
	referencedSet := buildReferencedSet(allViews)

	viewsByName := make(map[string]*v1.View, len(allViews))
	for i := range allViews {
		viewsByName[allViews[i].GetNamespacedName()] = &allViews[i]
	}

	var multi api.MultiViewResult
	for i := range allViews {
		v := &allViews[i]
		if referencedSet[v.GetNamespacedName()] {
			continue
		}

		result, err := runAndNormalize(ctx, v, vars)
		if err != nil {
			return nil, fmt.Errorf("view %s: %w", v.Name, err)
		}

		resolveViewRefs(ctx, result, viewsByName, vars)
		multi.Views = append(multi.Views, *result)
	}

	if len(multi.Views) == 1 {
		return formatResult(&multi.Views[0], format)
	}

	return formatMultiResult(&multi, format)
}

func buildReferencedSet(views []v1.View) map[string]bool {
	refs := make(map[string]bool)
	for _, v := range views {
		for _, s := range v.Spec.Sections {
			if s.ViewRef != nil {
				refs[fmt.Sprintf("%s/%s", s.ViewRef.Namespace, s.ViewRef.Name)] = true
			}
		}
	}
	return refs
}

func resolveViewRefs(ctx context.Context, result *api.ViewResult, viewsByName map[string]*v1.View, vars map[string]string) {
	for _, section := range result.Sections {
		if section.ViewRef == nil {
			continue
		}
		key := fmt.Sprintf("%s/%s", section.ViewRef.Namespace, section.ViewRef.Name)
		refView, ok := viewsByName[key]
		if !ok {
			continue
		}

		refResult, err := runAndNormalize(ctx, refView, vars)
		if err != nil {
			ctx.Logger.Warnf("failed to resolve viewRef %s: %v", key, err)
			continue
		}

		result.SectionResults = append(result.SectionResults, api.ViewSectionResult{
			Title: section.Title,
			Icon:  section.Icon,
			View:  refResult,
		})
	}
}

func runAndNormalize(ctx context.Context, view *v1.View, vars map[string]string) (*api.ViewResult, error) {
	request := &requestOpt{
		variables:   vars,
		includeRows: true,
	}
	result, err := Run(ctx, view, request)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}
	normalizeRows(result)
	return result, nil
}

func formatResult(result *api.ViewResult, format string) ([]byte, error) {
	switch format {
	case "csv":
		return renderViewCSV(result)
	case "html":
		return RenderHTML(result)
	case "pdf":
		return RenderPDF(result)
	case "facet-html":
		return RenderFacetHTML(result)
	case "facet-pdf":
		return RenderFacetPDF(result)
	default:
		return json.MarshalIndent(result, "", "  ")
	}
}

func formatMultiResult(multi *api.MultiViewResult, format string) ([]byte, error) {
	switch format {
	case "csv":
		return renderMultiViewCSV(multi)
	case "html":
		return RenderMultiHTML(multi)
	case "pdf":
		return RenderMultiPDF(multi)
	case "facet-html":
		return RenderMultiFacetHTML(multi)
	case "facet-pdf":
		return RenderMultiFacetPDF(multi)
	default:
		return json.MarshalIndent(multi, "", "  ")
	}
}

func Export(ctx context.Context, view *v1.View, vars map[string]string, format string) ([]byte, error) {
	request := &requestOpt{
		variables:   vars,
		includeRows: true,
	}

	result, err := Run(ctx, view, request)
	if err != nil {
		return nil, fmt.Errorf("failed to run view: %w", err)
	}

	normalizeRows(result)

	switch format {
	case "csv":
		return renderViewCSV(result)
	case "html":
		return RenderHTML(result)
	case "pdf":
		return RenderPDF(result)
	case "facet-html":
		return RenderFacetHTML(result)
	case "facet-pdf":
		return RenderFacetPDF(result)
	default:
		return json.MarshalIndent(result, "", "  ")
	}
}

// normalizeRows converts []byte and JSON-string cell values into proper
// Go objects so they serialize as JSON objects/arrays instead of strings.
func normalizeRows(result *api.ViewResult) {
	for i, row := range result.Rows {
		for j, cell := range row {
			result.Rows[i][j] = normalizeCell(cell)
		}
	}
}

func normalizeCell(v any) any {
	var raw []byte
	switch val := v.(type) {
	case []byte:
		raw = val
	case string:
		if len(val) == 0 || (val[0] != '{' && val[0] != '[') {
			return v
		}
		raw = []byte(val)
	default:
		return v
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return v
	}
	return parsed
}

func renderViewCSV(result *api.ViewResult) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)

	cols := make([]int, 0, len(result.Columns))
	headers := make([]string, 0, len(result.Columns))
	for i, c := range result.Columns {
		if c.Hidden || isInternalColumn(c) {
			continue
		}
		cols = append(cols, i)
		headers = append(headers, c.Name)
	}

	if err := w.Write(headers); err != nil {
		return nil, fmt.Errorf("write CSV header: %w", err)
	}

	for _, row := range result.Rows {
		record := make([]string, len(cols))
		for j, colIdx := range cols {
			if colIdx < len(row) {
				record[j] = fmt.Sprintf("%v", row[colIdx])
			}
		}
		if err := w.Write(record); err != nil {
			return nil, fmt.Errorf("write CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush CSV: %w", err)
	}

	return []byte(buf.String()), nil
}

func renderMultiViewCSV(multi *api.MultiViewResult) ([]byte, error) {
	var buf strings.Builder
	for i, v := range multi.Views {
		if i > 0 {
			buf.WriteString("\n")
		}
		data, err := renderViewCSV(&v)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}
	return []byte(buf.String()), nil
}
