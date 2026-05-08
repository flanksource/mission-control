package cmd

import (
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	dutyAPI "github.com/flanksource/duty/api"
)

const localJWTSecretFile = ".mission-control.jwt"

func ensureLocalJWTSecret() {
	if dutyAPI.DefaultConfig.Postgrest.JWTSecret != "" {
		return
	}

	if data, err := os.ReadFile(localJWTSecretFile); err == nil {
		if secret := strings.TrimSpace(string(data)); secret != "" {
			dutyAPI.DefaultConfig.Postgrest.JWTSecret = secret
			return
		}
	} else if !os.IsNotExist(err) {
		logger.Warnf("failed to read %s: %v", localJWTSecretFile, err)
	}

	secret := utils.RandomString(32)
	if err := os.WriteFile(localJWTSecretFile, []byte(secret+"\n"), 0600); err != nil {
		logger.Warnf("failed to write %s: %v", localJWTSecretFile, err)
		dutyAPI.DefaultConfig.Postgrest.JWTSecret = secret
		return
	}

	dutyAPI.DefaultConfig.Postgrest.JWTSecret = secret
	logger.Infof("generated PostgREST JWT secret at %s", localJWTSecretFile)
}
