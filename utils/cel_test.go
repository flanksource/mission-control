package utils

import (
	"testing"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	"github.com/samber/lo"
	"gotest.tools/v3/assert"
)

func TestMatchQuery(t *testing.T) {
	config := models.ConfigItem{
		Name: lo.ToPtr("aws-demo"),
	}

	runTests(t, []TestCase{
		{map[string]any{"config": config}, "matchQuery(config, 'name=aws*')", "true"},
		{map[string]any{"config": config}, "matchQuery(config, 'name=azure*')", "false"},
	})
}

type TestCase struct {
	env        map[string]interface{}
	expression string
	out        string
}

func runTests(t *testing.T, tests []TestCase) {
	for _, tc := range tests {
		t.Run(tc.expression, func(t *testing.T) {
			out, err := gomplate.RunTemplate(tc.env, gomplate.Template{
				CelEnvs:    CelFunctions,
				Expression: tc.expression,
			})

			assert.ErrorIs(t, nil, err)
			assert.Equal(t, tc.out, out)
		})
	}
}
