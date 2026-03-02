package cmd

import (
	"testing"

	"github.com/flanksource/duty/models"
	"github.com/onsi/gomega"
)

func TestBuildConnectionCRD(t *testing.T) {
	g := gomega.NewWithT(t)

	tests := []struct {
		name     string
		flags    connectionFlags
		expected string
	}{
		{
			name: "postgres with URL",
			flags: connectionFlags{
				Name:      "mydb",
				Namespace: "default",
				Type:      models.ConnectionTypePostgres,
				URL:       "postgres://user:pass@localhost:5432/db",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: mydb
  namespace: default
spec:
  postgres:
    url:
      value: postgres://user:pass@localhost:5432/db
`,
		},
		{
			name: "http with only URL",
			flags: connectionFlags{
				Name:      "httpbin",
				Namespace: "mc",
				Type:      models.ConnectionTypeHTTP,
				URL:       "https://httpbin.org/status/200",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: httpbin
  namespace: mc
spec:
  http:
    url: https://httpbin.org/status/200
`,
		},
		{
			name: "gemini with model",
			flags: connectionFlags{
				Name:      "my-gemini",
				Namespace: "default",
				Type:      models.ConnectionTypeGemini,
				ApiKey:    "GKEY123",
				Model:     "gemini-2.0-flash",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-gemini
  namespace: default
spec:
  gemini:
    apiKey:
      value: GKEY123
    model: gemini-2.0-flash
`,
		},
		{
			name: "slack",
			flags: connectionFlags{
				Name:      "test-slack",
				Namespace: "mc",
				Type:      models.ConnectionTypeSlack,
				Token:     "xoxb-123",
				Channel:   "C12345",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: test-slack
  namespace: mc
spec:
  slack:
    channel: C12345
    token:
      value: xoxb-123
`,
		},
		{
			name: "aws",
			flags: connectionFlags{
				Name:      "my-aws",
				Namespace: "default",
				Type:      models.ConnectionTypeAWS,
				AccessKey: "AKIA123",
				SecretKey: "mysecret",
				Region:    "us-east-1",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-aws
  namespace: default
spec:
  aws:
    accessKey:
      value: AKIA123
    region: us-east-1
    secretKey:
      value: mysecret
`,
		},
		{
			name: "folder",
			flags: connectionFlags{
				Name:      "artifacts",
				Namespace: "default",
				Type:      models.ConnectionTypeFolder,
				Path:      "/data/artifacts",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: artifacts
  namespace: default
spec:
  folder:
    path: /data/artifacts
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crd := buildConnectionCRD(&tt.flags)
			out, err := marshalConnectionCRD(crd)
			g.Expect(err).To(gomega.BeNil())
			g.Expect(string(out)).To(gomega.Equal(tt.expected))
		})
	}
}

func TestMarshalDryRunOutput(t *testing.T) {
	g := gomega.NewWithT(t)

	tests := []struct {
		name     string
		flags    connectionFlags
		expected string
	}{
		{
			name: "aws from-profile with session token",
			flags: connectionFlags{
				Name:         "my-aws",
				Namespace:    "mc",
				Type:         models.ConnectionTypeAWS,
				AccessKey:    "AKIA123",
				SecretKey:    "wJalrXU",
				SessionToken: "FwoGZX",
				Region:       "us-east-1",
				FromProfile:  "production",
			},
			expected: `apiVersion: v1
kind: Secret
metadata:
  name: my-aws
  namespace: mc
stringData:
  AWS_ACCESS_KEY_ID: AKIA123
  AWS_SECRET_ACCESS_KEY: wJalrXU
  AWS_SESSION_TOKEN: FwoGZX
---
apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-aws
  namespace: mc
spec:
  aws:
    accessKey:
      valueFrom:
        secretKeyRef:
          key: AWS_ACCESS_KEY_ID
          name: my-aws
    region: us-east-1
    secretKey:
      valueFrom:
        secretKeyRef:
          key: AWS_SECRET_ACCESS_KEY
          name: my-aws
    sessionToken:
      valueFrom:
        secretKeyRef:
          key: AWS_SESSION_TOKEN
          name: my-aws
`,
		},
		{
			name: "aws from-profile without session token",
			flags: connectionFlags{
				Name:        "my-aws",
				Namespace:   "mc",
				Type:        models.ConnectionTypeAWS,
				AccessKey:   "AKIA123",
				SecretKey:   "wJalrXU",
				Region:      "us-east-1",
				FromProfile: "default",
			},
			expected: `apiVersion: v1
kind: Secret
metadata:
  name: my-aws
  namespace: mc
stringData:
  AWS_ACCESS_KEY_ID: AKIA123
  AWS_SECRET_ACCESS_KEY: wJalrXU
---
apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-aws
  namespace: mc
spec:
  aws:
    accessKey:
      valueFrom:
        secretKeyRef:
          key: AWS_ACCESS_KEY_ID
          name: my-aws
    region: us-east-1
    secretKey:
      valueFrom:
        secretKeyRef:
          key: AWS_SECRET_ACCESS_KEY
          name: my-aws
`,
		},
		{
			name: "aws dry-run without from-profile",
			flags: connectionFlags{
				Name:      "my-aws",
				Namespace: "default",
				Type:      models.ConnectionTypeAWS,
				AccessKey: "AKIA123",
				SecretKey: "mysecret",
				Region:    "us-east-1",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-aws
  namespace: default
spec:
  aws:
    accessKey:
      value: AKIA123
    region: us-east-1
    secretKey:
      value: mysecret
`,
		},
		{
			name: "s3 from-profile",
			flags: connectionFlags{
				Name:        "my-s3",
				Namespace:   "mc",
				Type:        models.ConnectionTypeS3,
				AccessKey:   "AKIA456",
				SecretKey:   "secret456",
				Region:      "eu-west-1",
				Bucket:      "my-bucket",
				FromProfile: "dev",
			},
			expected: `apiVersion: v1
kind: Secret
metadata:
  name: my-s3
  namespace: mc
stringData:
  AWS_ACCESS_KEY_ID: AKIA456
  AWS_SECRET_ACCESS_KEY: secret456
---
apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: my-s3
  namespace: mc
spec:
  s3:
    accessKey:
      valueFrom:
        secretKeyRef:
          key: AWS_ACCESS_KEY_ID
          name: my-s3
    bucket: my-bucket
    region: eu-west-1
    secretKey:
      valueFrom:
        secretKeyRef:
          key: AWS_SECRET_ACCESS_KEY
          name: my-s3
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := marshalDryRunOutput(&tt.flags)
			g.Expect(err).To(gomega.BeNil())
			g.Expect(string(out)).To(gomega.Equal(tt.expected))
		})
	}
}
