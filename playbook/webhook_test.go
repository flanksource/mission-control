package playbook_test

import (
	"bytes"
	gocontext "context"
	"io"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Playbook Webhook", func() {
	type args struct {
		r       *http.Request
		headers map[string]string
		auth    *v1.PlaybookEventWebhookAuth
	}
	type test struct {
		name    string
		args    args
		wantErr bool
	}
	tests := []test{
		{
			name: "basic auth webhook verification",
			args: args{
				r: &http.Request{},
				headers: map[string]string{
					"Authorization": "Basic dXNlcjpwYXNz",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					Basic: &v1.PlaybookEventWebhookAuthBasic{
						Username: types.EnvVar{
							ValueStatic: "user",
						},
						Password: types.EnvVar{
							ValueStatic: "pass",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "basic auth webhook verification | FAIL",
			args: args{
				r: &http.Request{},
				headers: map[string]string{
					"Authorization": "Basic dXNlcjpwYXNz",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					Basic: &v1.PlaybookEventWebhookAuthBasic{
						Username: types.EnvVar{
							ValueStatic: "user",
						},
						Password: types.EnvVar{
							ValueStatic: "another-pass",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			// https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries#testing-the-webhook-payload-validation
			name: "github webhook verification",
			args: args{
				r: &http.Request{
					Body: io.NopCloser(bytes.NewBuffer([]byte("Hello, World!"))),
				},
				headers: map[string]string{
					"X-Hub-Signature-256": "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					Github: &v1.PlaybookEventWebhookAuthGithub{
						Token: types.EnvVar{
							ValueStatic: "It's a Secret to Everybody",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "github webhook verification | FAIL",
			args: args{
				r: &http.Request{
					Body: io.NopCloser(bytes.NewBuffer([]byte("Bye, World!"))),
				},
				headers: map[string]string{
					"X-Hub-Signature-256": "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					Github: &v1.PlaybookEventWebhookAuthGithub{
						Token: types.EnvVar{
							ValueStatic: "It's a Secret to Everybody",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "SVIX webhook verification",
			args: args{
				r: &http.Request{
					Body: io.NopCloser(bytes.NewBuffer([]byte(`{"test": 2432232314}`))),
				},
				headers: map[string]string{
					"svix-id":        "msg_p5jXN8AQM9LWM0D4loKWxJek",
					"svix-signature": "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=",
					"svix-timestamp": "1614265330",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					SVIX: &v1.PlaybookEventWebhookAuthSVIX{
						Secret: types.EnvVar{
							ValueStatic: "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
						},
					},
				},
			},
		},
		{
			name: "SVIX webhook verification | FAIL",
			args: args{
				r: &http.Request{
					Body: io.NopCloser(bytes.NewBuffer([]byte(`bad payload`))),
				},
				headers: map[string]string{
					"svix-id":        "msg_p5jXN8AQM9LWM0D4loKWxJek",
					"svix-signature": "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=",
					"svix-timestamp": "1614265330",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					SVIX: &v1.PlaybookEventWebhookAuthSVIX{
						Secret: types.EnvVar{
							ValueStatic: "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
						},
					},
				},
			},
			wantErr: true,
		},
		// {
		// 	name: "JWT webhook verification", TODO:
		// 	args: args{
		// 		r: &http.Request{
		// 			Body: io.NopCloser(bytes.NewBuffer([]byte(`{"payload": "test"}`))),
		// 		},
		// 		headers: map[string]string{
		// 			"Authorization": "Bearer eyJhbGciOiJSUzI1NiIsImp3a3NVcmkiOiJodHRwOi8vbG9jYWxob3N0OjIwMTkvandrcy5qc29uIiwia2lkIjoiZ25tQWZ2bWxzaTNrS0gzVmxNMUFKODVQMmhla1E4T05fWHZKcXMzeFBEOCJ9.eyJ0ZXN0IjoiZGF0YSJ9.E4JoQQL-FBvKKldws5RzX3TgYm-xdgVnNMrkL9Z6RrreOoR5aNuKpUQly45rRooi-lgzFKpEJa-I_dQhk-MnOlKj2s6EVThw-y7HZ6tiBAtKsc43LWXAUbttDXflVfu0uKU0HrZWFHqv3AGqmwlIw_m8q9Yndpy-TjvAl8-VoMKN-N7F3_XT9BtlioGBjlpbEjzcXCEr30yTjJ9zkueBFtoNNjj3luLWpMiPQa0PcGF6VGLi2b1QzwutCu7cil19XswBhl8jT1jvJBxcVVHxug1nedWwZgHaKTWp7euIyY11X_lSpwos0GUpy2K51bHJCi4HQ2jyMUN0KrEmY20FqQ",
		// 		},
		// 		auth: &v1.PlaybookEventWebhookAuth{
		// 			JWT: &v1.PlaybookEventWebhookAuthJWT{
		// 				JWKSURI: "http://localhost:2019/jwks.json",
		// 			},
		// 		},
		// 	},
		// },
		{
			name: "JWT webhook verification | FAIL",
			args: args{
				r: &http.Request{
					Body: io.NopCloser(bytes.NewBuffer([]byte(`{"payload": "test"}`))),
				},
				headers: map[string]string{
					"Authorization": "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.NHVaYe26MbtOYhSKkoKYdFVomg4i8ZJd8_-RU8VNbftc4TSMb4bXP3l3YlNWACwyXPGffz5aXHc6lty1Y2t4SWRqGteragsVdZufDn5BlnJl9pdR_kdVFUsra2rWKEofkZeIC4yWytE58sMIihvo9H1ScmmVwBcQP6XETqYd0aSHp1gOa9RdUPDvoXQ5oqygTqVtxaDr6wUFKrKItgBMzWIdNZ6y7O9E0DhEPTbE9rfBo6KTFsHAZnMg4k68CDp2woYIaXbmYTWcvbzIuHO7_37GT79XdIwkm95QJ7hYC9RiwrV7mesbY4PAahERJawntho0my942XheVLmGwLMBkQ",
				},
				auth: &v1.PlaybookEventWebhookAuth{
					JWT: &v1.PlaybookEventWebhookAuthJWT{
						JWKSURI: "https://raw.githubusercontent.com/MicahParks/keyfunc/ab22bfcd9495ed15e4b341c8490a296231ee1be1/example_jwks.json",
					},
				},
			},
			wantErr: true,
		},
	}

	var entries = []interface{}{
		func(tt test) {
			tt.args.r.Header = http.Header{}
			for k, v := range tt.args.headers {
				tt.args.r.Header.Set(k, v)
			}
			err := playbook.AuthenticateWebhook(context.NewContext(gocontext.TODO()), tt.args.r, tt.args.auth)
			if tt.wantErr {
				Expect(err).NotTo(BeNil())
			} else {
				Expect(err).To(BeNil())
			}
		},
	}

	for _, test := range tests {
		entries = append(entries, ginkgo.Entry(test.name, test))
	}

	ginkgo.DescribeTable("args", entries...)
})
