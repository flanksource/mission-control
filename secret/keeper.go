package secret

import (
	"fmt"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/samber/lo"
	"gocloud.dev/secrets"
	"gocloud.dev/secrets/awskms"
	"gocloud.dev/secrets/gcpkms"
)

var allowedConnectionTypes = []string{
	models.ConnectionTypeAWS,
	models.ConnectionTypeGCP,
	models.ConnectionTypeAzure,
	// Vault not supported yet
}

func GetKeeper(ctx context.Context, keeperURL, connectionString string) (*secrets.Keeper, error) {
	conn, err := ctx.HydrateConnectionByURL(connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	} else if conn == nil {
		return nil, fmt.Errorf("connection not found: %s", connectionString)
	}

	if !lo.Contains(allowedConnectionTypes, conn.Type) {
		return nil, fmt.Errorf("connection type %s cannot be used to create a secret keeper", conn.Type)
	}

	switch conn.Type {
	case models.ConnectionTypeAWS:
		var awsConn connection.AWSConnection
		awsConn.FromModel(*conn)

		awsConfig, err := awsConn.Client(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create AWS client: %w", err)
		}

		kmsClient, err := awskms.DialV2(awsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create AWS KMS client: %w", err)
		}

		keeper := awskms.OpenKeeperV2(kmsClient, keeperURL, nil)
		return keeper, nil

	case models.ConnectionTypeAzure:

	case models.ConnectionTypeGCP:
		var gcpConn connection.GCPConnection
		gcpConn.FromModel(*conn)

		oauthToken, err := gcpConn.TokenSource(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP oauth2 token: %w", err)
		}

		kmsClient, _, err := gcpkms.Dial(ctx, oauthToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP KMS client: %w", err)
		}

		keeper := gcpkms.OpenKeeper(kmsClient, keeperURL, nil)
		return keeper, nil
	}

	return nil, nil
}
