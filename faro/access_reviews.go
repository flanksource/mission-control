package main

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

var (
	accessReviewsUser  string
	accessReviewsSince string
	accessReviewsLimit int
)

// AccessReviews exports the recorded attestations of access grants.
var AccessReviews = &cobra.Command{
	Use:   "reviews [QUERY...]",
	Short: "Export recorded access reviews",
	Long: `Exports one row per recorded review of an access grant, newest first.

QUERY uses the same PEG search grammar as 'catalog search' and narrows the
export to the matching configs. Without a query every config is included.

Examples:
  faro access reviews --since 90d
  faro access reviews type=Azure::EnterpriseApplication --csv
  faro access reviews --user '*@example.com,!svc-*'`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ctx, err := accessClient()
		if err != nil {
			return err
		}

		configIDs, err := resolveConfigIDs(ctx, client, args)
		if err != nil {
			return err
		}
		userIDs, err := resolveUserIDs(ctx, client, accessReviewsUser)
		if err != nil {
			return err
		}
		since, err := parseSince(accessReviewsSince)
		if err != nil {
			return err
		}
		if accessReviewsUser != "" && len(userIDs) == 0 {
			printAccessResult(AccessReviewsResult{}, accessReviewRows(nil))
			return nil
		}

		reviews, total, err := client.ListAccessReviews(ctx, sdk.AccessHistoryOptions{
			ConfigIDs: configIDs,
			UserIDs:   userIDs,
			Since:     since,
			Limit:     accessReviewsLimit,
		})
		if err != nil {
			return err
		}
		warnTruncated("access reviews", len(reviews), total)

		printAccessResult(AccessReviewsResult{Rows: reviews}, accessReviewRows(reviews))
		return nil
	},
}

// AccessReviewsResult is the printable value for `access reviews`.
type AccessReviewsResult struct {
	Rows []sdk.AccessReview `json:"rows"`
}

func (r AccessReviewsResult) Pretty() api.Text {
	if len(r.Rows) == 0 {
		return clicky.Text("No access reviews found.", "text-gray-500")
	}
	return clicky.Text(fmt.Sprintf("Access reviews: %d", len(r.Rows)), "font-bold text-gray-700").
		NewLine().Append(api.NewTableFrom(accessReviewRows(r.Rows)))
}

func accessReviewRows(reviews []sdk.AccessReview) []accessReviewRow {
	return lo.Map(reviews, func(v sdk.AccessReview, _ int) accessReviewRow { return accessReviewRow{v} })
}

func init() {
	AccessReviews.Flags().StringVar(&accessReviewsUser, "user", "", "Filter reviewed users with MatchItem patterns")
	AccessReviews.Flags().StringVar(&accessReviewsSince, "since", "90d", "Only include reviews newer than this age (e.g. 24h, 30d, 1y)")
	AccessReviews.Flags().IntVar(&accessReviewsLimit, "limit", 100, "Maximum number of rows (0 for no limit)")
	clicky.BindAllFlags(AccessReviews.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access", AccessReviews)
}
