package testutils

import (
	"fmt"
	"io"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
)

func GetPGConfig(database string, port int) embeddedPG.Config {
	// We are firing up multiple instances of the embedded postgres server at once when running tests in parallel.
	//
	// By default fergusstrange/embedded-postgres directly extracts the Postgres binary to a set location
	// (/home/runner/.embedded-postgres-go/extracted/bin/initdb) and starts it.
	// If two instances try to do this at the same time, they conflict, and throw the error
	// "unable to extract postgres archive: open /home/runner/.embedded-postgres-go/extracted/bin/initdb: text file busy."
	//
	// This is a way to have separate instances of the running postgres servers.
	return embeddedPG.DefaultConfig().
		Database(database).
		Port(uint32(port)).
		RuntimePath(fmt.Sprintf("/tmp/.embedded-postgres-go/extracted-%d", port)).
		Logger(io.Discard)
}
