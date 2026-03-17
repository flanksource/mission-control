package utils

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Tail", func() {
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
		ginkgo.It(tt.name, func() {
			result := Tail(tt.data, tt.size)
			Expect(result).To(Equal(tt.expected))
		})
	}
})
