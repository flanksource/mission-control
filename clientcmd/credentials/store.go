// Package credentials persists Mission Control CLI secrets separately from
// config.json, with the durability guarantees a rotating single-use refresh
// token requires: writes are atomic, mutations are serialised across processes
// via WithLock, and Writable reports up-front whether a credential can be
// stored at all — before one is spent.
package credentials

import (
	"fmt"

	"github.com/flanksource/incident-commander/auth/oidcclient"
)

// Kinds of credential store. The kind in use is recorded in config.json and is
// never re-detected at runtime: silently falling back to the file store would
// write a plaintext refresh token the user asked to keep in the keychain.
const (
	KindFile     = "file"
	KindKeychain = "keychain"
)

// Credential is everything secret about a single context.
type Credential struct {
	Token string             `json:"token,omitempty"`
	OIDC  *oidcclient.Tokens `json:"oidc,omitempty"`

	// NeedsReauth is set when the server has terminally rejected the refresh
	// token. It is a durable marker: the credential cannot recover on its own
	// and the user must log in again.
	NeedsReauth string `json:"needs_reauth,omitempty"`
}

func (c *Credential) IsZero() bool {
	return c == nil || (c.Token == "" && c.OIDC == nil && c.NeedsReauth == "")
}

func (c *Credential) clone() *Credential {
	if c == nil {
		return nil
	}
	out := *c
	if c.OIDC != nil {
		tokens := *c.OIDC
		out.OIDC = &tokens
	}
	return &out
}

// Store holds the secrets for every context.
//
// Get is safe to call without the lock: writes land via an atomic rename, so a
// reader always sees a complete version. Set and Delete mutate and MUST be
// called with the store's lock held — see WithLock.
type Store interface {
	Name() string
	Get(context string) (*Credential, error)
	Set(context string, cred *Credential) error
	Delete(context string) error

	// Writable reports whether Set can durably persist, without mutating any
	// stored credential. Call it before spending a single-use token.
	Writable() error
}

// Open returns the store of the given kind rooted at dir. An empty kind means
// the file store, which is the only one that works everywhere.
func Open(dir, kind string) (Store, error) {
	switch kind {
	case "", KindFile:
		return NewFileStore(dir), nil
	case KindKeychain:
		return NewKeychainStore(dir), nil
	default:
		return nil, fmt.Errorf("unknown credential store %q (want %q or %q)", kind, KindFile, KindKeychain)
	}
}
