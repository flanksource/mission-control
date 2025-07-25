package actions

import (
	"fmt"
	"testing"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/onsi/gomega"

	"github.com/flanksource/duty/logs"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/samber/lo"
)

func Test_matchLogs(t *testing.T) {
	referenceTime := time.Now()
	ctx := context.New()

	tests := []struct {
		name        string
		postProcess v1.LogsPostProcess
		got         []*logs.LogLine
		want        []*logs.LogLine
	}{
		{
			name: "dedupe on message",
			postProcess: v1.LogsPostProcess{
				Match: []types.MatchExpression{
					"msg != 'new request'", // faulty expression
					"message == 'user saved'",
				},
			},
			got: []*logs.LogLine{
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       "user saved",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 5),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 1),
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       "user saved",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gg := gomega.NewWithT(t)
			result := postProcessLogs(ctx, tt.got, tt.postProcess)
			gg.Expect(len(result)).To(gomega.Equal(len(tt.want)))
			for i, log := range result {
				gg.Expect(log.Message).To(gomega.Equal(tt.want[i].Message))
				gg.Expect(log.FirstObserved).To(gomega.Equal(tt.want[i].FirstObserved), "first observed")
				gg.Expect(log.LastObserved).To(gomega.Equal(tt.want[i].LastObserved), "last observed")
				gg.Expect(log.Count).To(gomega.Equal(tt.want[i].Count), "count")
			}
		})
	}
}

func Test_dedupLogs(t *testing.T) {
	referenceTime := time.Date(2025, 5, 8, 12, 0, 0, 0, time.UTC)
	ctx := context.New()

	tests := []struct {
		name        string
		postProcess v1.LogsPostProcess
		got         []*logs.LogLine
		want        []*logs.LogLine
	}{
		{
			name: "dedupe on message",
			postProcess: v1.LogsPostProcess{
				Dedupe: &v1.LogDedupe{
					Fields: []string{"message"},
				},
			},
			got: []*logs.LogLine{
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       "user saved",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 5),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 1),
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 1)),
					Count:         3,
				},
				{
					Message:       "user saved",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
			},
		},
		{
			name: "dedupe on labels",
			postProcess: v1.LogsPostProcess{
				Dedupe: &v1.LogDedupe{
					Fields: []string{"label.namespace"},
				},
			},
			got: []*logs.LogLine{
				{
					Message: "new request",
					Labels: map[string]string{
						"namespace": "monitoring",
					},
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message: "user saved",
					Labels: map[string]string{
						"namespace": "monitoring",
					},
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       "user saved",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 10)),
					Count:         2,
				},
			},
		},
		{
			name: "dedupe on multiple fields",
			postProcess: v1.LogsPostProcess{
				Dedupe: &v1.LogDedupe{
					Fields: []string{"host", "message"},
				},
			},
			got: []*logs.LogLine{
				{
					Message:       "new request",
					Host:          "pod-a",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       "user saved",
					Host:          "pod-a",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
				{
					Message:       "new request",
					Host:          "pod-b",
					FirstObserved: referenceTime.Add(-time.Minute * 5),
					Count:         1,
				},
				{
					Message:       "new request",
					Host:          "pod-b",
					FirstObserved: referenceTime.Add(-time.Minute * 1),
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       "new request",
					Host:          "pod-a",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       "user saved",
					Host:          "pod-a",
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
				{
					Message:       "new request",
					Host:          "pod-b",
					FirstObserved: referenceTime.Add(-time.Minute * 5),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 1)),
					Count:         2,
				},
			},
		},
		{
			name: "dedupe on hash",
			postProcess: v1.LogsPostProcess{
				Dedupe: &v1.LogDedupe{
					Fields: []string{"hash"},
				},
			},
			got: []*logs.LogLine{
				{
					Message:       fmt.Sprintf("new request received: %s", referenceTime.Add(-time.Minute*20).Format(time.RFC3339)),
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       fmt.Sprintf("new request received: %s", referenceTime.Add(-time.Minute*10).Format(time.RFC3339)),
					FirstObserved: referenceTime.Add(-time.Minute * 10),
					Count:         1,
				},
				{
					Message:       fmt.Sprintf("new request received: %s", referenceTime.Add(-time.Minute*5).Format(time.RFC3339)),
					FirstObserved: referenceTime.Add(-time.Minute * 5),
					Count:         1,
				},
				{
					Message:       fmt.Sprintf("new request received: %s", referenceTime.Add(-time.Minute*1).Format(time.RFC3339)),
					FirstObserved: referenceTime.Add(-time.Minute * 1),
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       fmt.Sprintf("new request received: %s", referenceTime.Add(-time.Minute*1).Format(time.RFC3339)),
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 1)),
					Count:         4,
				},
			},
		},
		{
			name: "dedupe on message with a window",
			postProcess: v1.LogsPostProcess{
				Dedupe: &v1.LogDedupe{
					Fields: []string{"message"},
					Window: "5m",
				},
			},
			got: []*logs.LogLine{
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 17),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 14),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 13),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 11),
					Count:         1,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime,
					Count:         1,
				},
			},
			want: []*logs.LogLine{
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 20),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 17)),
					Count:         2,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime.Add(-time.Minute * 14),
					LastObserved:  lo.ToPtr(referenceTime.Add(-time.Minute * 11)),
					Count:         3,
				},
				{
					Message:       "new request",
					FirstObserved: referenceTime,
					Count:         1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gg := gomega.NewWithT(t)
			result := postProcessLogs(ctx, tt.got, tt.postProcess)
			gg.Expect(len(result)).To(gomega.Equal(len(tt.want)), "total logs")

			for i, log := range result {
				want := tt.want[i]

				gg.Expect(log.Message).To(gomega.Equal(want.Message))
				gg.Expect(log.Count).To(gomega.Equal(want.Count), "count")
				gg.Expect(log.FirstObserved).To(gomega.Equal(want.FirstObserved), "first observed")
				gg.Expect(log.LastObserved).To(gomega.Equal(want.LastObserved), "last observed")
				gg.Expect(log.Host).To(gomega.Equal(want.Host), "host")
			}
		})
	}
}
