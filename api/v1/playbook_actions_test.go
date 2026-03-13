package v1

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("NextRetryWait", func() {
	tests := []struct {
		name          string
		RetryCount    int
		Retry         PlaybookActionRetry
		ExpectedRange []float64
		ExpectedErr   bool
	}{
		{
			name:          "no jitter",
			RetryCount:    1,
			ExpectedRange: []float64{60, 60},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 2,
				},
				Jitter: 0,
			},
		},
		{
			name:          "no jitter second iteration",
			RetryCount:    2,
			ExpectedRange: []float64{120, 120},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 2,
				},
				Jitter: 0,
			},
		},
		{
			name:          "with jitter second iteration",
			RetryCount:    2,
			ExpectedRange: []float64{108, 132},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 2,
				},
				Jitter: 10,
			},
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			nextTime, err := tt.Retry.NextRetryWait(tt.RetryCount)
			if tt.ExpectedErr {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}

			Expect(nextTime.Seconds()).To(And(
				BeNumerically(">=", tt.ExpectedRange[0]),
				BeNumerically("<=", tt.ExpectedRange[1]),
			))
		})
	}
})
