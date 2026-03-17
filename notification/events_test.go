package notification

import (
	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("IsHealthReportable", func() {
	tests := []struct {
		name           string
		events         []string
		previousHealth models.Health
		currentHealth  models.Health
		expected       bool
	}{
		{
			name:           "health worsened",
			events:         []string{"config.warning", "config.healthy"},
			previousHealth: models.HealthHealthy,
			currentHealth:  models.HealthWarning,
			expected:       false,
		},
		{
			name:           "health changed and got better",
			events:         []string{"config.warning", "config.healthy"},
			previousHealth: models.HealthWarning,
			currentHealth:  models.HealthHealthy,
			expected:       false,
		},
		{
			name:           "Current health not in notification",
			events:         []string{"config.healthy"},
			previousHealth: models.HealthHealthy,
			currentHealth:  models.HealthUnhealthy,
			expected:       false,
		},
		{
			name:           "health unchanged",
			events:         []string{"config.warning", "config.healthy"},
			previousHealth: models.HealthHealthy,
			currentHealth:  models.HealthHealthy,
			expected:       true,
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			result := isHealthReportable(tt.events, tt.previousHealth, tt.currentHealth)
			Expect(result).To(Equal(tt.expected))
		})
	}
})
