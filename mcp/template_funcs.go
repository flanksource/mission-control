package mcp

import (
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/gomplate/v3/conv"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// duty's catalog.traverse and gitops.source read caches shared by every caller, so a row one caller read is served
// to the next without their row filters. RunTemplate adds every function in duty's global maps to every template,
// so the entries are replaced here: a template run under a caller's row filters gets versions that read through
// the caller's transaction, and every other template gets duty's functions unchanged.
// This package's variables are initialized after duty's init has filled the maps.
var (
	dutyTraverseCEL      = context.CelEnvFuncs["catalog.traverse"]
	dutyTraverseTemplate = context.TemplateFuncs["catalog_traverse"]
	dutyGitOpsCEL        = context.CelEnvFuncs["gitops.source"]
	dutyGitOpsTemplate   = context.TemplateFuncs["gitops_source"]
)

func init() {
	context.CelEnvFuncs["catalog.traverse"] = func(ctx context.Context) cel.EnvOption {
		if ctx.RLSPayload() == nil {
			return dutyTraverseCEL(ctx)
		}
		return cel.Function("catalog.traverse",
			cel.Overload("catalog.traverse_string_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
				cel.AnyType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					items := traverseVisible(ctx, lo.Map(args, func(a ref.Val, _ int) string { return conv.ToString(a) })...)
					jsonObj, _ := conv.AnyToListMapStringAny(items)
					return types.NewDynamicList(types.DefaultTypeAdapter, jsonObj)
				}),
			),
		)
	}
	context.TemplateFuncs["catalog_traverse"] = func(ctx context.Context) any {
		if ctx.RLSPayload() == nil {
			return dutyTraverseTemplate(ctx)
		}
		return func(args ...string) []models.ConfigItem {
			return traverseVisible(ctx, args...)
		}
	}

	// gitops.source follows relationships through duty's caches in several steps, so under row filters it returns
	// an empty source instead.
	context.CelEnvFuncs["gitops.source"] = func(ctx context.Context) cel.EnvOption {
		if ctx.RLSPayload() == nil {
			return dutyGitOpsCEL(ctx)
		}
		return cel.Function("gitops.source",
			cel.Overload("gitops.source_interface{}",
				[]*cel.Type{cel.DynType},
				cel.DynType,
				cel.UnaryBinding(func(ref.Val) ref.Val {
					return types.DefaultTypeAdapter.NativeToValue((&query.GitOpsSource{}).AsMap())
				}),
			),
		)
	}
	context.TemplateFuncs["gitops_source"] = func(ctx context.Context) any {
		if ctx.RLSPayload() == nil {
			return dutyGitOpsTemplate(ctx)
		}
		return func(...any) map[string]any {
			return (&query.GitOpsSource{}).AsMap()
		}
	}
}

// traverseVisible follows query.TraverseConfig's walk, with arguments (id, relation type[, direction]), but reads
// related ids and configs through ctx.DB(), so it returns only configs the caller can read.
func traverseVisible(ctx context.Context, args ...string) []models.ConfigItem {
	if len(args) < 2 || len(args) > 3 {
		return nil
	}
	direction := query.Incoming
	if len(args) == 3 {
		direction = query.RelationDirection(args[2])
	}
	// A query error would abort the caller's transaction for the rest of the template
	if _, err := uuid.Parse(args[0]); err != nil {
		return nil
	}

	ids := []string{args[0]}
	for _, typ := range strings.Split(args[1], "/") {
		var next []string
		for _, id := range ids {
			var related []string
			if err := ctx.DB().Table("related_configs_recursive(?, ?)", id, direction).Select("id").Where("type = ?", typ).Scan(&related).Error; err != nil {
				ctx.Tracef("error querying related_configs[%s]: %v", id, err)
				return nil
			}
			next = append(next, lo.Without(related, id)...)
		}
		ids = next
	}
	if len(ids) == 0 {
		return nil
	}

	var configs []models.ConfigItem
	if err := ctx.DB().Where("id IN ?", ids).Where("deleted_at IS NULL").Find(&configs).Error; err != nil {
		ctx.Tracef("error querying configs %v: %v", ids, err)
		return nil
	}
	byID := lo.KeyBy(configs, func(c models.ConfigItem) string { return c.ID.String() })
	return lo.FilterMap(ids, func(id string, _ int) (models.ConfigItem, bool) {
		c, ok := byID[id]
		return c, ok
	})
}
