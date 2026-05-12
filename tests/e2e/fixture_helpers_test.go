package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/duty/logs"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/cel-go/cel"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/artifacts"
)

// --- Fixture types ---

type fixtureRef struct {
	Ref    string
	Inline json.RawMessage
}

func (r *fixtureRef) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		r.Ref = s
		return nil
	}
	var raw map[string]any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	r.Inline, _ = json.Marshal(raw)
	return nil
}

func (r *fixtureRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		r.Ref = s
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Inline = data
	return nil
}

func (r *fixtureRef) resolve(basePath string, target any) error {
	if r.Ref != "" {
		content, err := os.ReadFile(filepath.Join(filepath.Dir(basePath), r.Ref))
		if err != nil {
			return err
		}
		return yaml.Unmarshal(content, target)
	}
	return json.Unmarshal(r.Inline, target)
}

type fixtureSetup struct {
	Loki        bool         `yaml:"loki"`
	OpenSearch  bool         `yaml:"opensearch"`
	Facet       bool         `yaml:"facet"`
	SMTP        bool         `yaml:"smtp"`
	Connections []fixtureRef `yaml:"connections"`
	Permissions []fixtureRef `yaml:"permissions"`
}

type playbookFixture struct {
	Playbook   v1.PlaybookSpec   `yaml:"playbook"`
	Config     string            `yaml:"config"`
	Setup      fixtureSetup      `yaml:"setup"`
	Params     map[string]string `yaml:"params"`
	Output     expectedOutput    `yaml:"output"`
	Assertions []string          `yaml:"assertions"`
}

type expectedOutput struct {
	Run     expectedRun      `yaml:"run"`
	Actions []expectedAction `yaml:"actions"`
}

type expectedRun struct {
	Status string `yaml:"status"`
}

type expectedAction struct {
	Name         string             `yaml:"name" json:"name"`
	Status       string             `yaml:"status" json:"status"`
	Result       map[string]string  `yaml:"result" json:"result"`
	Artifacts    []expectedArtifact `yaml:"artifacts" json:"artifacts"`
	ArtifactLogs string             `yaml:"artifact_logs" json:"artifact_logs"`
}

type expectedArtifact struct {
	ContentType string `yaml:"content_type" json:"content_type"`
}

func loadPlaybookFixture(path string) playbookFixture {
	content, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	var f playbookFixture
	Expect(yaml.Unmarshal(content, &f)).To(Succeed())
	return f
}

func peekFixtureSetup(path string) fixtureSetup {
	content, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("failed to read fixture %s: %v", path, err))
	}
	var partial struct {
		Setup struct {
			Loki       bool `yaml:"loki"`
			OpenSearch bool `yaml:"opensearch"`
			Facet      bool `yaml:"facet"`
			SMTP       bool `yaml:"smtp"`
		} `yaml:"setup"`
	}
	if err := yaml.Unmarshal(content, &partial); err != nil {
		panic(fmt.Sprintf("failed to parse fixture %s: %v", path, err))
	}
	return fixtureSetup{
		Loki:       partial.Setup.Loki,
		OpenSearch: partial.Setup.OpenSearch,
		Facet:      partial.Setup.Facet,
		SMTP:       partial.Setup.SMTP,
	}
}

func resolveParams(params map[string]string, vars map[string]string) map[string]string {
	resolved := make(map[string]string, len(params))
	for k, v := range params {
		for placeholder, val := range vars {
			v = strings.ReplaceAll(v, "{{"+placeholder+"}}", val)
		}
		resolved[k] = v
	}
	return resolved
}

func fixtureToPlaybook(f playbookFixture) v1.Playbook {
	name := strings.ToLower(strings.ReplaceAll(f.Playbook.Title, " ", "-"))
	namespace := "default"
	uid := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("playbook-fixture:%s/%s/%s", namespace, name, f.Playbook.Category)))

	return v1.Playbook{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "mission-control.flanksource.com/v1",
			Kind:       "Playbook",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid.String()),
		},
		Spec: f.Playbook,
	}
}

func resolveConfigID(name string) uuid.UUID {
	switch name {
	case "LogisticsAPIPodConfig":
		return dummy.LogisticsAPIPodConfig.ID
	case "EKSCluster":
		return dummy.EKSCluster.ID
	default:
		ginkgo.Fail(fmt.Sprintf("unknown config fixture: %s", name))
		return uuid.Nil
	}
}

func compareOutput(expected expectedOutput, run *models.PlaybookRun, actions []models.PlaybookRunAction) {
	Expect(string(run.Status)).To(Equal(expected.Run.Status), "run status mismatch")
	Expect(actions).To(HaveLen(len(expected.Actions)), "action count mismatch")

	for i, ea := range expected.Actions {
		actual := actions[i]
		Expect(actual.Name).To(Equal(ea.Name), "action[%d] name", i)
		Expect(string(actual.Status)).To(Equal(ea.Status), "action[%d] status", i)

		for k, v := range ea.Result {
			Expect(actual.Result[k]).To(Equal(v), "action[%d] result[%s]", i, k)
		}

		if len(ea.Artifacts) > 0 {
			var dbArtifacts []models.Artifact
			Expect(DefaultContext.DB().Where("playbook_run_action_id = ?", actual.ID).Find(&dbArtifacts).Error).To(Succeed())
			Expect(dbArtifacts).To(HaveLen(len(ea.Artifacts)), "action[%d] artifact count", i)
			for j, eArt := range ea.Artifacts {
				Expect(dbArtifacts[j].ContentType).To(Equal(eArt.ContentType), "action[%d] artifact[%d] content_type", i, j)
			}
		}

		if ea.ArtifactLogs != "" {
			contents, err := artifacts.GetArtifactContents(DefaultContext.WithSubject("admin"), actual.ID.String())
			Expect(err).ToNot(HaveOccurred(), "action[%d] artifact contents", i)
			Expect(contents).To(HaveLen(1), "action[%d] expected 1 artifact for log comparison", i)

			var lines []logs.LogLine
			Expect(json.Unmarshal(contents[0].Content, &lines)).To(Succeed(), "action[%d] unmarshal log lines", i)

			seen := make(map[string]bool)
			var output strings.Builder
			for _, line := range lines {
				if !seen[line.Message] {
					seen[line.Message] = true
					output.WriteString(line.Message)
					output.WriteString("\n")
				}
			}
			Expect(output.String()).To(Equal(ea.ArtifactLogs), "action[%d] artifact logs", i)
		}
	}
}

func evalAssertions(assertions []string, env map[string]any) {
	if len(assertions) == 0 {
		return
	}

	envOpts := []cel.EnvOption{
		cel.Variable("emails", cel.ListType(cel.MapType(cel.StringType, cel.DynType))),
	}
	celEnv, err := cel.NewEnv(envOpts...)
	Expect(err).ToNot(HaveOccurred())

	for _, expr := range assertions {
		ast, issues := celEnv.Compile(expr)
		Expect(issues.Err()).ToNot(HaveOccurred(), "CEL compile %q", expr)

		prg, err := celEnv.Program(ast)
		Expect(err).ToNot(HaveOccurred(), "CEL program %q", expr)

		out, _, err := prg.Eval(env)
		Expect(err).ToNot(HaveOccurred(), "CEL eval %q", expr)
		Expect(out.Value()).To(BeTrue(), "CEL assertion failed: %s", expr)
	}
}
