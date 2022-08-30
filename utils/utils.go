package utils

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"

	"github.com/flanksource/commons/logger"
)

func GetHash(any interface{}) string {
	data, err := json.Marshal(any)
	if err != nil {
		logger.Debugf("error marshalling the given input: %v", err)
		return ""
	}
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}
