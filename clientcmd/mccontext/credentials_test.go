package mccontext

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/incident-commander/clientcmd/credentials"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// legacyConfigJSON is the pre-store shape: secrets inline in each context.
const legacyConfigJSON = `{
  "current_context": "beta",
  "contexts": [
    {
      "name": "beta",
      "server": "https://beta.example.com/api",
      "oidc": {
        "access_token": "legacy-access",
        "refresh_token": "legacy-refresh",
        "id_token": "legacy-id",
        "expires_at": "2030-01-01T00:00:00Z"
      }
    },
    {
      "name": "ci",
      "server": "https://ci.example.com/api",
      "token": "legacy-static-token"
    }
  ]
}`

func writeConfigJSON(contents string) {
	Expect(os.MkdirAll(configDir(), 0700)).To(Succeed())
	Expect(os.WriteFile(configPath(), []byte(contents), 0600)).To(Succeed())
}

func readConfigJSON() map[string]any {
	data, err := os.ReadFile(configPath())
	Expect(err).ToNot(HaveOccurred())
	var out map[string]any
	Expect(json.Unmarshal(data, &out)).To(Succeed())
	return out
}

var _ = ginkgo.Describe("credential migration", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("moves inline secrets into the file store and out of config.json", func() {
		writeConfigJSON(legacyConfigJSON)

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.CredentialStore).To(Equal("file"))
		Expect(cfg.GetContext("beta").OIDC.RefreshToken).To(Equal("legacy-refresh"))
		Expect(cfg.GetContext("ci").Token).To(Equal("legacy-static-token"))

		config, err := os.ReadFile(configPath())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(config)).ToNot(ContainSubstring("legacy-refresh"))
		Expect(string(config)).ToNot(ContainSubstring("legacy-access"))
		Expect(string(config)).ToNot(ContainSubstring("legacy-static-token"))

		contexts := readConfigJSON()["contexts"].([]any)
		Expect(contexts).To(HaveLen(2))
		for _, ctx := range contexts {
			Expect(ctx).ToNot(HaveKey("oidc"))
			Expect(ctx).ToNot(HaveKey("token"))
		}

		creds, err := os.ReadFile(filepath.Join(configDir(), "credentials.json"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(creds)).To(ContainSubstring("legacy-refresh"))
		Expect(string(creds)).To(ContainSubstring("legacy-static-token"))
	})

	// Migration must not resurrect a credential that has since been rotated or
	// removed: the store always wins once it holds an entry.
	ginkgo.It("does not overwrite a credential the store already holds", func() {
		writeConfigJSON(legacyConfigJSON)
		Expect(LoadConfig()).ToNot(BeNil())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		cfg.GetContext("beta").OIDC.RefreshToken = "rotated-refresh"
		Expect(SaveConfig(cfg)).To(Succeed())

		// A stale config.json reappears — a restored backup, or another tool.
		writeConfigJSON(legacyConfigJSON)

		reloaded, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.GetContext("beta").OIDC.RefreshToken).To(Equal("rotated-refresh"))
	})

	// A read-only config mount and a CI container with a baked-in token must keep
	// working: the secrets stay where they already were rather than the whole
	// config becoming unloadable.
	ginkgo.It("keeps working when the inline secrets cannot be migrated", func() {
		if os.Geteuid() == 0 {
			ginkgo.Skip("root bypasses directory permissions")
		}

		writeConfigJSON(legacyConfigJSON)
		Expect(os.Chmod(configDir(), 0500)).To(Succeed())
		defer func() { _ = os.Chmod(configDir(), 0700) }()

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.GetContext("ci").Token).To(Equal("legacy-static-token"))
		Expect(cfg.GetContext("beta").OIDC.RefreshToken).To(Equal("legacy-refresh"))
		Expect(filepath.Join(configDir(), "credentials.json")).ToNot(BeAnExistingFile())
	})

	ginkgo.It("leaves a config with no inline secrets alone", func() {
		writeConfigJSON(`{"current_context":"beta","contexts":[{"name":"beta","server":"https://beta.example.com/api"}]}`)

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.CredentialStore).To(BeEmpty())
		Expect(cfg.GetContext("beta").HasAuth()).To(BeFalse())
		Expect(filepath.Join(configDir(), "credentials.json")).ToNot(BeAnExistingFile())
	})
})

var _ = ginkgo.Describe("SaveConfig credential sync", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("persists a secret set on an in-memory config", func() {
		Expect(SaveConfig(&MCConfig{
			CurrentContext: "beta",
			Contexts:       []MCContext{{Name: "beta", Server: "https://beta.example.com/api", Token: "static-token"}},
		})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.GetContext("beta").Token).To(Equal("static-token"))
	})

	ginkgo.It("removes the credential of a context that was removed", func() {
		Expect(SaveConfig(&MCConfig{
			Contexts: []MCContext{
				{Name: "beta", Server: "https://beta.example.com/api", Token: "beta-token"},
				{Name: "ci", Server: "https://ci.example.com/api", Token: "ci-token"},
			},
		})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.RemoveContext("beta")).To(BeTrue())
		Expect(SaveConfig(cfg)).To(Succeed())

		creds, err := os.ReadFile(filepath.Join(configDir(), "credentials.json"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(creds)).ToNot(ContainSubstring("beta-token"))
		Expect(string(creds)).To(ContainSubstring("ci-token"))
	})

	// Switching backends must move the secrets, not orphan them behind a config
	// that no longer points at the old store. Two file stores in separate
	// directories stand in for two backends — same contract, no keychain.
	ginkgo.It("moves credentials across when the store kind changes", func() {
		Expect(SaveConfig(&MCConfig{
			CredentialStore: "file",
			Contexts: []MCContext{
				{Name: "beta", Server: "https://beta.example.com/api", Token: "beta-token"},
				{Name: "ci", Server: "https://ci.example.com/api", Token: "ci-token"},
			},
		})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.GetContext("beta").Token).To(Equal("beta-token"))

		before, err := credentials.Open(configDir(), "file")
		Expect(err).ToNot(HaveOccurred())
		after := credentials.NewFileStore(ginkgo.GinkgoT().TempDir())

		Expect(cfg.syncCredentials(after, before)).To(Succeed())

		for _, name := range []string{"beta", "ci"} {
			Expect(after.Get(name)).ToNot(BeNil(), name)
			Expect(before.Get(name)).To(BeNil(), name)
		}
		Expect(cfg.loadedStore).To(Equal(cfg.CredentialStore))
	})

	ginkgo.It("clears the credential when a context's secret is cleared", func() {
		Expect(SaveConfig(&MCConfig{
			Contexts: []MCContext{{Name: "beta", Server: "https://beta.example.com/api", Token: "beta-token"}},
		})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		cfg.GetContext("beta").Token = ""
		Expect(SaveConfig(cfg)).To(Succeed())

		reloaded, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.GetContext("beta").HasAuth()).To(BeFalse())

		creds, err := os.ReadFile(filepath.Join(configDir(), "credentials.json"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(creds)).ToNot(ContainSubstring("beta-token"))
	})
})
