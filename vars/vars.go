package vars

var (
	AuthMode = ""

	// SecretKeeperConnection is the connection to the key management service
	// that's used to encrypt and decrypt secrets.
	SecretKeeperConnection string
)

const FlagRLSEnable = "rls.enable"
