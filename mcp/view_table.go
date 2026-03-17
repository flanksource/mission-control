package mcp

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/db"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"

	"github.com/flanksource/incident-commander/api"
)

func readViewRows(ctx context.Context, namespace, name, fingerprint string, columns []string, page, size int) ([]map[string]any, error) {
	offset := (page - 1) * size
	if offset < 0 {
		offset = 0
	}

	clauses := []clause.Expression{
		clause.Limit{Limit: &size, Offset: offset},
		clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: "request_fingerprint", Value: fingerprint},
		}},
	}

	if len(columns) > 0 {
		clauses = append(clauses,
			clause.Select{Columns: lo.Map(columns, func(c string, _ int) clause.Column {
				return clause.Column{Name: c}
			})},
		)
	}

	records, err := db.ReadTable(ctx.DB(), models.View{Namespace: namespace, Name: name}.GeneratedTableName(), clauses...)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func normalizePagination(opts *viewRequest, viewResult *api.ViewResult) {
	if opts.page < 1 {
		opts.page = viewDefaultPage
	}

	if opts.limit <= 0 {
		if viewResult.Table != nil && viewResult.Table.Size > 0 {
			opts.limit = viewResult.Table.Size
		} else {
			opts.limit = viewDefaultLimit
		}
	}

	if opts.limit > viewMaxLimit {
		opts.limit = viewMaxLimit
	}
}
