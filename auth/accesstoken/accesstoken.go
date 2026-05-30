package accesstoken

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/flanksource/duty/secret"
	"github.com/flanksource/incident-commander/auth/signing"
	"golang.org/x/crypto/argon2"
)

const (
	// The draft RFC(https://tools.ietf.org/html/draft-irtf-cfrg-argon2-03#section-9.3) recommends
	// the following time and memory cost as sensible defaults.
	timeCost    = 1
	memoryCost  = 64 * 1024
	parallelism = 4
	keyLength   = 20
	saltLength  = 12
)

type Token struct {
	Password    string
	Salt        string
	TimeCost    uint32
	MemoryCost  uint32
	Parallelism uint8
	JWK         string
}

// "password.jwk.salt.timeCost.memoryCost.parallelism" to user, stores base64(hash)
func (t Token) V2() secret.Sensitive {
	b64JWK := base64.URLEncoding.EncodeToString([]byte(t.JWK))
	return secret.Sensitive(fmt.Appendf(nil, "%s.%s.%s.%d.%d.%d", t.Password, b64JWK, t.Salt, t.TimeCost, t.MemoryCost, t.Parallelism))
}

//	"password.salt.timeCost.memoryCost.parallelism"
//
// Deprecated: New tokens must be V2
func (t Token) V1() secret.Sensitive {
	return secret.Sensitive(fmt.Appendf(nil, "%s.%s.%d.%d.%d", t.Password, t.Salt, t.TimeCost, t.MemoryCost, t.Parallelism))
}

// Hash of the token that's saved in the database
func (t Token) Hash() string {
	var inputPassword = t.Password
	if t.JWK != "" {
		// We cryptographically bind the jwk with the password as a way to pin this access token to the JWK
		inputPassword = fmt.Sprintf("%s.%s", t.Password, t.JWK)
	}

	hash := argon2.IDKey([]byte(inputPassword), []byte(t.Salt), t.TimeCost, t.MemoryCost, t.Parallelism, keyLength)
	encodedHash := base64.URLEncoding.EncodeToString(hash)
	return encodedHash
}

// Generate generates a new access token from a randomy generated password + salt.
// It returns two side of the token
// a token to be saved on the database and another one to be passed to a user
func Generate() (Token, error) {
	jwk, err := signing.PublicJWK()
	if err != nil {
		return Token{}, err
	}

	var passwordRaw = make([]byte, saltLength)
	if _, err := rand.Read(passwordRaw); err != nil {
		return Token{}, err
	}
	password := base64.URLEncoding.EncodeToString(passwordRaw)

	var saltRaw = make([]byte, saltLength)
	if _, err := rand.Read(saltRaw); err != nil {
		return Token{}, err
	}
	salt := base64.URLEncoding.EncodeToString(saltRaw)

	token := Token{
		Password:    password,
		Salt:        salt,
		TimeCost:    timeCost,
		MemoryCost:  memoryCost,
		Parallelism: parallelism,
		JWK:         jwk,
	}

	return token, nil
}
