package clientcmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
	"github.com/flanksource/incident-commander/clientcmd/mccontext"
	"github.com/spf13/cobra"
)

var ContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage Mission Control contexts",
}

var contextUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Switch the current context",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := mccontext.LoadConfig()
		if err != nil {
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			if len(cfg.Contexts) == 0 {
				return fmt.Errorf("no contexts configured")
			}
			options := make([]huh.Option[string], len(cfg.Contexts))
			for i, c := range cfg.Contexts {
				label := c.Name
				if c.Name == cfg.CurrentContext {
					label += " (current)"
				}
				if c.Server != "" {
					label += "  " + c.Server
				}
				options[i] = huh.NewOption(label, c.Name)
			}
			if err := huh.NewSelect[string]().
				Title("Select context").
				Options(options...).
				Value(&name).
				Run(); err != nil {
				return err
			}
		}

		if cfg.GetContext(name) == nil {
			return fmt.Errorf("context %q not found", name)
		}

		cfg.CurrentContext = name
		if err := mccontext.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", name)
		return nil
	},
}

var contextListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all contexts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := mccontext.LoadConfig()
		if err != nil {
			return err
		}
		if len(cfg.Contexts) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured")
			return nil
		}
		for _, c := range cfg.Contexts {
			marker := "  "
			if c.Name == cfg.CurrentContext {
				marker = "* "
			}
			info := c.Server
			if info == "" && c.DB != "" {
				info = "(db only)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%s\n", marker, c.Name, info)
		}
		return nil
	},
}

var contextRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a Mission Control context",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := mccontext.LoadConfig()
		if err != nil {
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			if len(cfg.Contexts) == 0 {
				return fmt.Errorf("no contexts configured")
			}
			options := make([]huh.Option[string], len(cfg.Contexts))
			for i, c := range cfg.Contexts {
				label := c.Name
				if c.Name == cfg.CurrentContext {
					label += " (current)"
				}
				if c.Server != "" {
					label += "  " + c.Server
				}
				options[i] = huh.NewOption(label, c.Name)
			}
			if err := huh.NewSelect[string]().
				Title("Remove context").
				Options(options...).
				Value(&name).
				Run(); err != nil {
				return err
			}
		}

		previousCurrent := cfg.CurrentContext
		if !cfg.RemoveContext(name) {
			return fmt.Errorf("context %q not found", name)
		}
		if err := mccontext.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed context %q\n", name)
		if previousCurrent == name && cfg.CurrentContext != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", cfg.CurrentContext)
		}
		return nil
	},
}

var contextCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current context",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := mccontext.LoadConfig()
		if err != nil {
			return err
		}
		ctx := cfg.CurrentMCContext()
		if ctx == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "No current context")
			return nil
		}
		data, _ := json.MarshalIndent(ctx, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var (
	contextAddName   string
	contextAddServer string
	contextAddDB     string
	contextAddToken  string
	contextAddUse    bool
)

var contextAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add or update a Mission Control context",
	Long: `Add a new context (or update an existing one by name). At least one of --server
or --db-url is required. Pass --use to switch to the new context immediately.

Examples:
  mission-control context add --name local --db-url "$DB_URL"
  mission-control context add --name beta --server https://beta.flanksource.com --token "$TOKEN" --use`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if contextAddName == "" {
			return fmt.Errorf("--name is required")
		}
		if contextAddServer == "" && contextAddDB == "" {
			return fmt.Errorf("at least one of --server or --db-url is required")
		}

		cfg, err := mccontext.LoadConfig()
		if err != nil {
			return err
		}

		existingCtx := cfg.GetContext(contextAddName)
		existing := existingCtx != nil
		ctx := mccontext.MCContext{Name: contextAddName}
		if existingCtx != nil {
			ctx = *existingCtx
		}
		if cmd.Flags().Changed("server") {
			server, err := mccontext.ResolveAPIBase(contextAddServer)
			if err != nil {
				return err
			}
			ctx.Server = server
			if !cmd.Flags().Changed("token") {
				ctx.Token = ""
				ctx.OIDC = nil
				ctx.NeedsReauth = ""
			}
		}
		if cmd.Flags().Changed("db-url") {
			ctx.DB = contextAddDB
		}
		if cmd.Flags().Changed("token") {
			ctx.Token = contextAddToken
			ctx.OIDC = nil
			ctx.NeedsReauth = ""
		}
		if err := mccontext.ChooseCredentialStore(cfg, ""); err != nil {
			return err
		}
		if ctx.Server != "" && !cmd.Flags().Changed("token") && !ctx.HasAuth() {
			if err := EnsureContextToken(cmd, &ctx, cmd.ErrOrStderr()); err != nil {
				return err
			}
		}
		cfg.SetContext(ctx)

		if contextAddUse || cfg.CurrentContext == "" {
			cfg.CurrentContext = contextAddName
		}

		if err := mccontext.SaveConfig(cfg); err != nil {
			return err
		}

		action := "Added"
		if existing {
			action = "Updated"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s context %q\n", action, contextAddName)
		if cfg.CurrentContext == contextAddName {
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", contextAddName)
		}
		return nil
	},
}

func EnsureContextToken(cmd *cobra.Command, ctx *mccontext.MCContext, status io.Writer) error {
	if ctx == nil || ctx.Server == "" || ctx.AccessToken() != "" {
		return nil
	}
	if ctx.OIDC != nil && ctx.OIDC.RefreshToken != "" {
		if token, err := mccontext.ResolveContextToken(ctx); err == nil && token != "" {
			return nil
		}
	}

	var lastErr error
	for _, loginServer := range mccontext.OIDCLoginServerCandidates(ctx.Server) {
		fmt.Fprintf(status, "No token configured for context %q; starting OIDC login for %s\n", ctx.Name, loginServer)
		tokens, endpoints, err := oidcLogin(cmd, loginServer, status)
		if err == nil {
			ctx.SetOIDCTokens(tokens)
			ctx.Endpoints = endpoints
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("OAuth login failed for %s: %w", ctx.Server, lastErr)
}

func init() {
	contextAddCmd.Flags().StringVar(&contextAddName, "name", "", "Context name (required)")
	contextAddCmd.Flags().StringVar(&contextAddServer, "server", "", "Mission Control server URL")
	contextAddCmd.Flags().StringVar(&contextAddDB, "db-url", "", "Direct database connection URL")
	contextAddCmd.Flags().StringVar(&contextAddToken, "token", "", "API token for the server")
	contextAddCmd.Flags().BoolVar(&contextAddUse, "use", false, "Switch to this context after adding")

	ContextCmd.AddCommand(contextUseCmd, contextListCmd, contextCurrentCmd, contextAddCmd, contextRemoveCmd)
}
