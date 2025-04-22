package utils

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestTail(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		size     int
		expected []byte
	}{
		{
			name:     "empty slice",
			data:     []byte{},
			size:     5,
			expected: []byte{},
		},
		{
			name:     "size larger than data",
			data:     []byte{1, 2, 3},
			size:     5,
			expected: []byte{1, 2, 3},
		},
		{
			name:     "size equal to data length",
			data:     []byte{1, 2, 3},
			size:     3,
			expected: []byte{1, 2, 3},
		},
		{
			name:     "size smaller than data",
			data:     []byte{1, 2, 3, 4, 5},
			size:     3,
			expected: []byte{3, 4, 5},
		},
		{
			name:     "size zero",
			data:     []byte{1, 2, 3},
			size:     0,
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			result := Tail(tt.data, tt.size)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}
