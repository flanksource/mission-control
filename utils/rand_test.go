package utils

import (
	"testing"
)

func TestGenerateRandHex(t *testing.T) {
	tests := []struct {
		name    string
		length  int
		wantLen int
		wantErr bool
	}{
		{name: "odd", length: 1, wantErr: true},
		{name: "negative", length: -1, wantErr: true},
		{name: "even", length: 2, wantLen: 2},
		{name: "even-long", length: 200, wantLen: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateRandHex(tt.length)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("Got = %v, want %v", len(got), tt.wantLen)
			}
		})
	}
}

func TestGenerateRandString(t *testing.T) {
	tests := []struct {
		name    string
		length  int
		wantLen int
		wantErr bool
	}{
		{name: "negative", length: -1, wantErr: true},
		{name: "odd", length: 1, wantLen: 1},
		{name: "even", length: 2, wantLen: 2},
		{name: "even-long", length: 200, wantLen: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateRandString(tt.length)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("Got = %v, want %v", len(got), tt.wantLen)
			}
		})
	}
}
