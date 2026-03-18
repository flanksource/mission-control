package actions

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/logs"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type logLineYAML struct {
	Message       string            `yaml:"message"`
	FirstObserved string            `yaml:"firstObserved"`
	Count         int               `yaml:"count"`
	Severity      string            `yaml:"severity,omitempty"`
	Source        string            `yaml:"source,omitempty"`
	Host          string            `yaml:"host,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
}

type logFixture struct {
	PostProcess  v1.LogsPostProcess `yaml:"postProcess"`
	Input        []logLineYAML      `yaml:"input"`
	Expectations []string           `yaml:"expectations"`
}

func loadLogFixture(path string) logFixture {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred(), "reading fixture %s", path)

	var fixture logFixture
	Expect(yaml.Unmarshal(data, &fixture)).To(Succeed(), "parsing fixture %s", path)
	return fixture
}

func fixtureToLogLines(input []logLineYAML) []*logs.LogLine {
	lines := make([]*logs.LogLine, len(input))
	for i, in := range input {
		t, err := time.Parse(time.RFC3339, in.FirstObserved)
		Expect(err).ToNot(HaveOccurred())
		lines[i] = &logs.LogLine{
			Message:       in.Message,
			FirstObserved: t,
			Count:         in.Count,
			Severity:      in.Severity,
			Source:        in.Source,
			Host:          in.Host,
			Labels:        in.Labels,
		}
		lines[i].SetHash()
	}
	return lines
}

func logLinesToCELContext(results []*logs.LogLine, messageFields []string) map[string]any {
	var items []map[string]any
	for _, r := range results {
		items = append(items, r.TemplateContext(messageFields...))
	}
	return map[string]any{"results": items}
}

var _ = ginkgo.Describe("fixture-based postProcessLogs", func() {
	ctx := context.New()

	files, err := os.ReadDir("testdata/logs")
	if err != nil {
		return
	}

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}

		name := f.Name()
		ginkgo.It(name, func() {
			fixture := loadLogFixture(filepath.Join("testdata/logs", name))
			input := fixtureToLogLines(fixture.Input)
			result := postProcessLogs(ctx, input, fixture.PostProcess)

			var messageFields []string
			if fixture.PostProcess.Mapping != nil {
				messageFields = fixture.PostProcess.Mapping.Message
			}
			env := logLinesToCELContext(result, messageFields)

			for _, expr := range fixture.Expectations {
				ok, err := gomplate.RunTemplateBool(env, gomplate.Template{Expression: expr})
				Expect(err).ToNot(HaveOccurred(), "CEL expression: %s", expr)
				Expect(ok).To(BeTrue(), "CEL failed: %s (results=%v)", expr, env["results"])
			}
		})
	}
})
