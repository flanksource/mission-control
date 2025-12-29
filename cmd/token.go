package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/flanksource/commons/rand"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/vars"
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

		token, _, err := db.CreateAccessToken(ctx, user.ID, "default", password, &tokenExpiry, nil, false)
		if err != nil {
			return fmt.Errorf("failed to create a new access token: %w", err)
		}

		fmt.Println(token)
		return nil
	},
}

var Check = &cobra.Command{
	Use:    "check subject action resource",
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

var PasswordReset = &cobra.Command{
	Use:   "password-reset",
	Short: "Reset a user's password (reads new password from stdin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if resetPasswordUser == "" {
			return errors.New("must specify --user")
		}

		password, err := readPasswordFromStdin()
		if err != nil {
			return err
		}

		switch vars.AuthMode {
		case auth.Basic:
			return resetBasicPassword(resetPasswordUser, password, auth.HtpasswdFile)
		case auth.Kratos:
			return resetKratosPassword(resetPasswordUser, password)
		default:
			return fmt.Errorf("unsupported auth mode: %s (supported: %s, %s)", vars.AuthMode, auth.Basic, auth.Kratos)
		}
	},
}

var (
	tokenUser         string
	tokenExpiry       time.Duration
	resetPasswordUser string
)

func readPasswordFromStdin() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	password, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("failed to read password from stdin: %w", err)
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	return password, nil
}

func resetKratosPassword(user, password string) error {
	ctx, stop, err := duty.Start("mission-control", duty.DisablePostgrest)
	if err != nil {
		return err
	}
	defer stop()

	var identity struct {
		ID       string        `gorm:"column:id"`
		Traits   types.JSONMap `gorm:"column:traits"`
		State    string        `gorm:"column:state"`
		SchemaID string        `gorm:"column:schema_id"`
	}
	if err := ctx.DB().Table("identities").
		Select("id, traits, state, schema_id").
		Where("traits->>'email' = ?", user).
		Scan(&identity).Error; err != nil {
		return fmt.Errorf("failed to find identity: %w", err)
	}
	if identity.ID == "" {
		return fmt.Errorf("user %s not found", user)
	}

	kratosHandler := auth.NewKratosHandler()
	if err := kratosHandler.ResetPassword(ctx, identity.ID, password, identity.Traits, identity.State, identity.SchemaID); err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	fmt.Printf("Password reset successfully for user: %s\n", user)
	return nil
}

func resetBasicPassword(user, password, htpasswdFile string) error {
	content, err := os.ReadFile(htpasswdFile)
	if err != nil {
		return fmt.Errorf("failed to read htpasswd file: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	newEntry := fmt.Sprintf("%s:%s", user, string(hash))

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, user+":") {
			lines[i] = newEntry
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("user %s not found in htpasswd file", user)
	}

	if err := os.WriteFile(htpasswdFile, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		return fmt.Errorf("failed to write htpasswd file: %w", err)
	}

	fmt.Printf("Password reset successfully for user: %s\n", user)
	return nil
}

func init() {
	Auth.AddCommand(Token, Check, PasswordReset)

	Token.Flags().StringVar(&tokenUser, "user", "", "User to generate a token for")
	Token.Flags().DurationVar(&tokenExpiry, "expiry", time.Hour*4, "Expiry duration for token")

	PasswordReset.Flags().StringVar(&resetPasswordUser, "user", "", "User email to reset password for")
	PasswordReset.Flags().StringVar(&vars.AuthMode, "auth", "", "Enable authentication via Kratos or Clerk. Valid values are [kratos, clerk, basic]")
	PasswordReset.Flags().StringVar(&auth.KratosAdminAPI, "kratos-admin", "http://kratos-admin:80", "Kratos Admin API service")

	Root.AddCommand(Auth)
}
