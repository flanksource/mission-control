module github.com/flanksource/mission-control-plugin-test

go 1.26.1

require github.com/flanksource/incident-commander/plugin/sdk v0.0.0

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/flanksource/incident-commander/plugin/api v0.0.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.8.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/oklog/run v1.1.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace github.com/flanksource/incident-commander/plugin/api => ../../../../../../plugin/api

replace github.com/flanksource/incident-commander/plugin/sdk => ../../../../../../plugin/sdk
