package api

import (
	sdk "github.com/flanksource/canary-checker/sdk"
)

var Topology *sdk.TopologyApiService

func init() {
	Topology = sdk.NewAPIClient(&sdk.Configuration{
		BasePath: CanaryCheckerPath,
	}).TopologyApi
}
