package utils

import (
	"testing"
	"time"
)

func TestParseCacheControlHeader(t *testing.T) {
	tests := []struct {
		name                   string
		cacheControl           string
		expectedMaxAge         time.Duration
		expectedRefreshTimeout time.Duration
		expectError            bool
	}{
		{
			name:                   "empty header",
			cacheControl:           "",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            false,
		},
		{
			name:                   "max-age only",
			cacheControl:           "max-age=300",
			expectedMaxAge:         300 * time.Second,
			expectedRefreshTimeout: 0,
			expectError:            false,
		},
		{
			name:                   "refresh-timeout only",
			cacheControl:           "refresh-timeout=60",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 60 * time.Second,
			expectError:            false,
		},
		{
			name:                   "both max-age and refresh-timeout",
			cacheControl:           "max-age=300, refresh-timeout=60",
			expectedMaxAge:         300 * time.Second,
			expectedRefreshTimeout: 60 * time.Second,
			expectError:            false,
		},
		{
			name:                   "with spaces around values",
			cacheControl:           "max-age = 450 , refresh-timeout = 90",
			expectedMaxAge:         0, // regex expects no spaces around =
			expectedRefreshTimeout: 0,
			expectError:            false,
		},
		{
			name:                   "with other cache directives",
			cacheControl:           "no-cache, max-age=180, private, refresh-timeout=30",
			expectedMaxAge:         180 * time.Second,
			expectedRefreshTimeout: 30 * time.Second,
			expectError:            false,
		},
		{
			name:                   "invalid max-age value - should error",
			cacheControl:           "max-age=invalid",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            true,
		},
		{
			name:                   "invalid refresh-timeout value - should error",
			cacheControl:           "refresh-timeout=invalid",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            true,
		},
		{
			name:                   "zero values",
			cacheControl:           "max-age=0, refresh-timeout=0",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            false,
		},
		{
			name:                   "large values",
			cacheControl:           "max-age=3600, refresh-timeout=300",
			expectedMaxAge:         3600 * time.Second,
			expectedRefreshTimeout: 300 * time.Second,
			expectError:            false,
		},
		{
			name:                   "no matching directives",
			cacheControl:           "no-cache, private, must-revalidate",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            false,
		},
		{
			name:                   "negative max-age - should error",
			cacheControl:           "max-age=-100",
			expectedMaxAge:         0,
			expectedRefreshTimeout: 0,
			expectError:            true,
		},
		{
			name:                   "both with different order",
			cacheControl:           "refresh-timeout=120, max-age=600",
			expectedMaxAge:         600 * time.Second,
			expectedRefreshTimeout: 120 * time.Second,
			expectError:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxAge, refreshTimeout, err := ParseCacheControlHeader(tt.cacheControl)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if maxAge != tt.expectedMaxAge {
				t.Errorf("maxAge = %v, expected %v", maxAge, tt.expectedMaxAge)
			}

			if refreshTimeout != tt.expectedRefreshTimeout {
				t.Errorf("refreshTimeout = %v, expected %v", refreshTimeout, tt.expectedRefreshTimeout)
			}
		})
	}
}
