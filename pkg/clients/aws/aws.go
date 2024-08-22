package aws

import (
	"crypto/tls"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/henvic/httpretty"
)

func GetAWSConfig(ctx *context.Context, conn connection.AWSConnection) (cfg aws.Config, err error) {
	var options []func(*config.LoadOptions) error

	if conn.Region != "" {
		options = append(options, config.WithRegion(conn.Region))
	}

	if conn.Endpoint != "" {
		// nolint:staticcheck // TODO: use the client from duty
		options = append(options, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...any) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL: conn.Endpoint,
				}, nil
			},
		)))
	}

	if !conn.AccessKey.IsEmpty() {
		options = append(options, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(conn.AccessKey.ValueStatic, conn.SecretKey.ValueStatic, "")))
	}

	if conn.SkipTLSVerify {
		var tr http.RoundTripper
		if ctx.IsTrace() {
			httplogger := &httpretty.Logger{
				Time:           true,
				TLS:            false,
				RequestHeader:  false,
				RequestBody:    false,
				ResponseHeader: true,
				ResponseBody:   false,
				Colors:         true,
				Formatters:     []httpretty.Formatter{&httpretty.JSONFormatter{}},
			}
			tr = httplogger.RoundTripper(tr)
		} else {
			tr = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}

		options = append(options, config.WithHTTPClient(&http.Client{Transport: tr}))
	}

	return config.LoadDefaultConfig(ctx, options...)
}
