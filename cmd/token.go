package cmd

import (
	gocontext "context"
	"fmt"
	"time"

	"github.com/flanksource/commons/rand"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var Auth = &cobra.Command{
	Use: "auth",
}

var Token = &cobra.Command{
	Use:    "token",
	PreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		password, err := rand.GenerateRandHex(32)
		if err != nil {
			return err
		}
		if tokenUser == "" {
			return fmt.Errorf("Must specify --user")
		}
		ctx := context.NewContext(gocontext.Background()).
			WithDB(db.Gorm, db.Pool)

		var user models.Person
		if err := ctx.DB().Where("email = ?", tokenUser).First(&user).Error; err != nil || user.ID == uuid.Nil {
			return fmt.Errorf("User not found")
		}
		token, err := db.CreateAccessToken(ctx, user.ID, "default", password, tokenExpiry)
		if err != nil {
			return fmt.Errorf("failed to create a new access token: %w", err)
		}

		fmt.Println(token)
		return nil
	},
}

var Check = &cobra.Command{
	Use:    "check",
	PreRun: PreRun,
	Args:   cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.NewContext(gocontext.Background()).
			WithDB(db.Gorm, db.Pool)
		fmt.Println(rbac.Check(ctx, args[0], args[1], args[2]))
		return nil
	},
}

var tokenUser string
var tokenExpiry time.Duration

func init() {
	Auth.AddCommand(Token, Check)
	Token.Flags().StringVar(&tokenUser, "user", "", "User to generate a token for")
	Token.Flags().DurationVar(&tokenExpiry, "expiry", time.Hour*4, "Expiry duration for token")
	Root.AddCommand(Auth)
}
