package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

type MCContext struct {
	Name       string            `json:"name"`
	Server     string            `json:"server,omitempty"`
	DB         string            `json:"db,omitempty"`
	Token      string            `json:"token,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type MCConfig struct {
	CurrentContext string      `json:"current_context"`
	Contexts       []MCContext `json:"contexts"`
}

var contextFlag string

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "mission-control")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func ProfileDir(namespace, name string) string {
	return filepath.Join(configDir(), "profiles", namespace+"_"+name)
}

func LoadConfig() (*MCConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &MCConfig{}, nil
		}
		return nil, err
	}
	var cfg MCConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &cfg, nil
}

func SaveConfig(cfg *MCConfig) error {
	if err := os.MkdirAll(filepath.Dir(configPath()), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func (c *MCConfig) GetContext(name string) *MCContext {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

func (c *MCConfig) SetContext(ctx MCContext) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == ctx.Name {
			c.Contexts[i] = ctx
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
}

func (c *MCConfig) CurrentMCContext() *MCContext {
	if contextFlag != "" {
		return c.GetContext(contextFlag)
	}
	if c.CurrentContext == "" {
		return nil
	}
	return c.GetContext(c.CurrentContext)
}

func ServerToContextName(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(serverURL)
	}
	return u.Hostname()
}

func contextHasAPI() (*MCContext, bool) {
	cfg, _ := LoadConfig()
	if cfg == nil {
		return nil, false
	}
	ctx := cfg.CurrentMCContext()
	return ctx, ctx != nil && ctx.Server != "" && ctx.Token != ""
}

var ContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage Mission Control contexts",
}

var contextUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Switch the current context",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
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
		if err := SaveConfig(cfg); err != nil {
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
		cfg, err := LoadConfig()
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

var contextCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current context",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := LoadConfig()
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

func init() {
	ContextCmd.AddCommand(contextUseCmd, contextListCmd, contextCurrentCmd)
	Root.AddCommand(ContextCmd)
	Root.PersistentFlags().StringVar(&contextFlag, "context", "", "Mission Control context to use")
}
