package topology

import (
	sdk "github.com/flanksource/canary-checker/sdk"

	"github.com/flanksource/incident-commander/api"
)

var topologyService *sdk.TopologyApiService

func Service() *sdk.TopologyApiService {
	if topologyService == nil {
		topologyService = sdk.NewAPIClient(&sdk.Configuration{
			BasePath: api.CanaryCheckerPath,
		}).TopologyApi
	}
	return topologyService
}
