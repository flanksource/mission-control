package v1

import (
	"testing"
)

func TestNextRetryWait(t *testing.T) {
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
			ExpectedRange: []float64{45, 45},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 1.5,
				},
				Jitter: 0,
			},
		},
		{
			name:          "no jitter second iteration",
			RetryCount:    2,
			ExpectedRange: []float64{67.5, 67.5},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 1.5,
				},
				Jitter: 0,
			},
		},
		{
			name:          "with jitter second iteration",
			RetryCount:    2,
			ExpectedRange: []float64{60, 75},
			Retry: PlaybookActionRetry{
				Limit:    1,
				Duration: "30s",
				Exponent: RetryExponent{
					Multiplier: 1.5,
				},
				Jitter: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextTime, err := tt.Retry.NextRetryWait(tt.RetryCount)
			if (err != nil) != tt.ExpectedErr {
				t.Errorf("expected error: %v, got error: %v", tt.ExpectedErr, err)
			}

			if nextTime.Seconds() < tt.ExpectedRange[0] || nextTime.Seconds() > tt.ExpectedRange[1] {
				t.Errorf("expected next time to be between %f and %f, got %v", tt.ExpectedRange[0], tt.ExpectedRange[1], nextTime)
			}
		})
	}
}
