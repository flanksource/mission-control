package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons/rand"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/flanksource/incident-commander/db"
)

var Auth = &cobra.Command{
	Use: "auth",
}

var Token = &cobra.Command{
	Use:    "token",
	PreRun: PreRun,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, _, err := duty.Start("mission-control", duty.DisablePostgrest)
		if err != nil {
			return err
		}
		password, err := rand.GenerateRandHex(32)
		if err != nil {
			return err
		}
		if tokenUser == "" {
			return errors.New("must specify --user")
		}

		var user models.Person
		if err := ctx.DB().Where("email = ?", tokenUser).First(&user).Error; err != nil || user.ID == uuid.Nil {
			return errors.New("user not found")
		}

		token, _, err := db.CreateAccessToken(ctx, user.ID, "default", password, &tokenExpiry, nil)
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
		ctx, _, err := duty.Start("mission-control", duty.DisablePostgrest)
		if err != nil {
			return err
		}
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
