package testutils

import (
	"fmt"
	"io"
	"math/rand"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
)

func GetPGConfig(database string, port int) embeddedPG.Config {
	nonce := rand.Int31n(1000)
	return embeddedPG.DefaultConfig().
		Database(database).
		Port(uint32(port)).
		RuntimePath(fmt.Sprintf("/tmp/.embedded-postgres-go/extracted-%d-%d", port, nonce)).
		Logger(io.Discard)
}
