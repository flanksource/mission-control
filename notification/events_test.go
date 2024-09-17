package notification

import (
	"testing"

	"github.com/flanksource/duty/models"
)

func TestIsHealthReportable(t *testing.T) {
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
			expected:       true,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHealthReportable(tt.events, tt.previousHealth, tt.currentHealth)
			if result != tt.expected {
				t.Errorf("isHealthReportable(%v, %v, %v) = %v; want %v",
					tt.events, tt.previousHealth, tt.currentHealth, result, tt.expected)
			}
		})
	}
}
