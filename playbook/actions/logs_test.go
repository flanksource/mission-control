package actions

import (
	"testing"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/logs"
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
				Dedupe: []string{"message"},
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
				Dedupe: []string{"label.namespace"},
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
				Dedupe: []string{"host", "message"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gg := gomega.NewWithT(t)
			result := postProcessLogs(ctx, tt.got, tt.postProcess)
			gg.Expect(len(result)).To(gomega.Equal(len(tt.want)))
			for i, log := range result {
				want := tt.want[i]

				gg.Expect(log.Message).To(gomega.Equal(want.Message))
				gg.Expect(log.FirstObserved).To(gomega.Equal(want.FirstObserved), "first observed")
				gg.Expect(log.LastObserved).To(gomega.Equal(want.LastObserved), "last observed")
				gg.Expect(log.Count).To(gomega.Equal(want.Count), "count")
				gg.Expect(log.Host).To(gomega.Equal(want.Host), "host")
			}
		})
	}
}
