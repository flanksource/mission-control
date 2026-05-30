package accesstoken

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

const (
	v1Parts = 5
	v2Parts = 6
)

var ErrInvalidFormat = errors.New("invalid access token format")

func Parse(accessToken string) (Token, error) {
	fields := strings.Split(accessToken, ".")

	switch len(fields) {
	case v1Parts:
		var (
			password = fields[0]
			salt     = fields[1]
		)

		timeCost, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		memoryCost, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		parallelism, err := strconv.ParseUint(fields[4], 10, 8)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		return Token{
			Password:    password,
			Salt:        salt,
			TimeCost:    uint32(timeCost),
			MemoryCost:  uint32(memoryCost),
			Parallelism: uint8(parallelism),
		}, nil

	case v2Parts:
		var (
			password = fields[0]
			salt     = fields[2]
		)

		jwk, err := base64.URLEncoding.DecodeString(fields[1])
		if err != nil {
			return Token{}, err
		}

		timeCost, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		memoryCost, err := strconv.ParseUint(fields[4], 10, 32)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		parallelism, err := strconv.ParseUint(fields[5], 10, 8)
		if err != nil {
			return Token{}, ErrInvalidFormat
		}

		return Token{
			Password:    password,
			Salt:        salt,
			JWK:         string(jwk),
			TimeCost:    uint32(timeCost),
			MemoryCost:  uint32(memoryCost),
			Parallelism: uint8(parallelism),
		}, nil

	default:
		return Token{}, ErrInvalidFormat
	}
}
