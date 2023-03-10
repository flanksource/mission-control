package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/flanksource/commons/logger"
)

func GetHash(obj any) string {
	data, err := json.Marshal(obj)
	if err != nil {
		logger.Debugf("error marshalling the given input: %v", err)
		return ""
	}
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

func Sha256Hex(in string) string {
	hash := sha256.New()
	hash.Write([]byte(in))
	hashVal := hash.Sum(nil)
	return hex.EncodeToString(hashVal[:])
}
