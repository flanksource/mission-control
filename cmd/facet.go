// Registers the facet server flags shared by the report exporting commands.
// A property that is already set wins over the flag.
package cmd

import (
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/report"
)

var (
	facetConnection string
	facetURL        string
)

// addFacetFlags registers the facet server flags on a report exporting command.
func addFacetFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&facetConnection, "facet-connection", "", "Facet connection to render reports with, e.g. mission-control/facet")
	cmd.Flags().StringVar(&facetURL, "facet-url", "", "Facet server URL to render reports with")
}

// applyFacetFlags seeds the facet properties from the flags. A property set in
// mission-control.properties or in the database takes precedence over the flag,
// which is the opposite of how --artifact-connection behaves.
func applyFacetFlags(ctx context.Context) {
	for _, flag := range []struct {
		property string
		value    string
	}{
		{report.PropertyConnection, facetConnection},
		{report.PropertyURL, facetURL},
	} {
		if flag.value == "" || ctx.Properties().String(flag.property, "") != "" {
			continue
		}
		properties.Set(flag.property, flag.value)
		ctx.ClearCache()
	}
}
