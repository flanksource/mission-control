package incidents

import (
	"strconv"
	"testing"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func Test_duration_func(t *testing.T) {
	testData := []struct {
		name               string
		script             string
		output             bool
		createdAt          time.Time
		lastTransitionTime time.Time
	}{
		{
			name:               "age compare | duration in seconds | fail",
			script:             `check.status == 'healthy' && check.age > duration("16s")`,
			lastTransitionTime: time.Now().Add(-time.Second * 15),
			output:             false,
		},
		{
			name:               "age compare | duration in seconds | pass",
			script:             `check.status == 'healthy' && check.age > duration("15s")`,
			lastTransitionTime: time.Now().Add(-time.Second * 20),
			output:             true,
		},
		{
			name:               "age compare | duration in minutes | fail",
			script:             `check.status == 'healthy' && check.age > duration("10m")`,
			lastTransitionTime: time.Now().Add(-time.Minute * 5),
			output:             false,
		},
		{
			name:               "age compare | duration in minutes | pass",
			script:             `check.status == 'healthy' && check.age > duration("10m")`,
			lastTransitionTime: time.Now().Add(-time.Minute * 15),
			output:             true,
		},
		// {
		// 	name:               `duration days pass | not supported by cel (Supported: "h" (hour), "m" (minute), "s" (second), "ms" (millisecond), "us" (microsecond), and "ns" (nanosecond)). (https://github.com/google/cel-spec/blob/master/doc/langdef.md)`,
		// 	script:             `check.status == 'healthy' && check.age > duration("10d")`,
		// 	lastTransitionTime: time.Now().Add(-time.Hour * 24 * 15),
		// 	output:             true,
		// },
	}

	for _, td := range testData {
		evidence := db.EvidenceScriptInput{
			Evidence: api.Evidence{
				Script:           td.script,
				DefinitionOfDone: true,
			},
			Check: api.Check{
				LastTransitionTime: td.lastTransitionTime,
				Status:             "healthy",
				CreatedAt:          td.createdAt,
				LastRuntime:        time.Now().Add(-time.Hour),
			},
		}

		out, err := evaluate(evidence)
		if err != nil {
			t.Fatalf("[%s] unexpected error: %v", td.name, err)
		}

		output, _ := strconv.ParseBool(out)
		if output != td.output {
			t.Fatalf("[%s] expected %v, got %v", td.name, td.output, out)
		}
	}
}
