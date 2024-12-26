package utils

import "github.com/samber/oops"

func MatchOopsErrCode(err error, code string) bool {
	if err == nil {
		return false
	}

	oe, ok := oops.AsOops(err)
	if !ok {
		return false
	}

	return oe.Code() == code
}
