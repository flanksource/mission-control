package clientcmd

import (
	"github.com/flanksource/duty/models"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("BuildConnectionCRD", func() {
	tests := []struct {
		name     string
		flags    ConnectionFlags
		expected string
	}{
		{
			name: "postgres with URL",
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			name: "elasticsearch",
			flags: ConnectionFlags{
				Name:        "search",
				Namespace:   "default",
				Type:        models.ConnectionTypeElasticSearch,
				URL:         "https://search.example.com",
				Username:    "elastic",
				Password:    "secret",
				InsecureTLS: true,
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: search
  namespace: default
spec:
  elasticsearch:
    insecureTLS: true
    password:
      value: secret
    url: https://search.example.com
    username:
      value: elastic
`,
		},
		{
			name: "redis",
			flags: ConnectionFlags{
				Name:      "cache",
				Namespace: "default",
				Type:      models.ConnectionTypeRedis,
				URL:       "redis.example.com:6379",
				Username:  "app",
				Password:  "secret",
			},
			expected: `apiVersion: mission-control.flanksource.com/v1
kind: Connection
metadata:
  name: cache
  namespace: default
spec:
  redis:
    password:
      value: secret
    url: redis.example.com:6379
    username:
      value: app
`,
		},
		{
			name: "folder",
			flags: ConnectionFlags{
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
		ginkgo.It("should build CRD for "+tt.name, func() {
			crd := buildConnectionCRD(&tt.flags)
			out, err := marshalConnectionCRD(crd)
			Expect(err).To(BeNil())
			Expect(string(out)).To(Equal(tt.expected))
		})
	}
})

var _ = ginkgo.Describe("ElasticSearch and Redis connection commands", func() {
	for _, connectionType := range []string{models.ConnectionTypeElasticSearch, models.ConnectionTypeRedis} {
		ginkgo.It("registers and builds "+connectionType+" connections", func() {
			cmd, _, err := ConnectionAdd.Find([]string{connectionType})
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd).NotTo(BeNil())
			Expect(cmd.Flags().Lookup("url")).NotTo(BeNil())
			Expect(cmd.Flags().Lookup("username")).NotTo(BeNil())
			Expect(cmd.Flags().Lookup("password")).NotTo(BeNil())

			flags := ConnectionFlags{Type: connectionType, URL: "service.example.com", Username: "app", Password: "secret"}
			Expect(validateConnectionFlags(&flags)).To(Succeed())
			connection, err := BuildConnectionFromFlags(&flags)
			Expect(err).NotTo(HaveOccurred())
			Expect(connection.Type).To(Equal(connectionType))
			Expect(connection.URL).To(Equal("service.example.com"))
			Expect(connection.Username).To(Equal("app"))
			Expect(connection.Password).To(Equal("secret"))
		})
	}

	ginkgo.It("adds insecure TLS only to ElasticSearch", func() {
		elasticsearch, _, err := ConnectionAdd.Find([]string{models.ConnectionTypeElasticSearch})
		Expect(err).NotTo(HaveOccurred())
		Expect(elasticsearch.Flags().Lookup("insecure-tls")).NotTo(BeNil())

		redis, _, err := ConnectionAdd.Find([]string{models.ConnectionTypeRedis})
		Expect(err).NotTo(HaveOccurred())
		Expect(redis.Flags().Lookup("insecure-tls")).To(BeNil())
	})
})

var _ = ginkgo.Describe("MarshalDryRunOutput", func() {
	tests := []struct {
		name     string
		flags    ConnectionFlags
		expected string
	}{
		{
			name: "aws from-profile with session token",
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
			flags: ConnectionFlags{
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
		ginkgo.It("should marshal dry-run output for "+tt.name, func() {
			out, err := marshalDryRunOutput(&tt.flags)
			Expect(err).To(BeNil())
			Expect(string(out)).To(Equal(tt.expected))
		})
	}
})
