package agent

import "testing"

func Test_generateRandomString(t *testing.T) {
	type args struct {
		length int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test1",
			args: args{
				length: 8,
			},
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateRandomString(tt.args.length); got != tt.want {
				t.Errorf("generateRandomString() = %v, want %v", got, tt.want)
			}
		})
	}
}
