package cmd

import (
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("local whoami access token hashing", func() {
	ginkgo.It("accepts valid and internally normalized Argon2 costs", func() {
		hash, err := hashMissionControlAccessToken("password.salt.1.0.1")
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).NotTo(BeEmpty())
	})

	tests := []struct {
		name        string
		timeCost    uint32
		memoryCost  uint32
		parallelism uint8
	}{
		{name: "zero time", timeCost: 0, memoryCost: 64 * 1024, parallelism: 4},
		{name: "excessive time", timeCost: maxAccessTokenTimeCost + 1, memoryCost: 64 * 1024, parallelism: 4},
		{name: "excessive memory", timeCost: 1, memoryCost: maxAccessTokenMemoryCost + 1, parallelism: 4},
		{name: "zero parallelism", timeCost: 1, memoryCost: 64 * 1024, parallelism: 0},
		{name: "excessive parallelism", timeCost: 1, memoryCost: 64 * 1024, parallelism: maxAccessTokenParallelism + 1},
	}
	for _, tt := range tests {
		ginkgo.It("rejects "+tt.name, func() {
			token := fmt.Sprintf("password.salt.%d.%d.%d", tt.timeCost, tt.memoryCost, tt.parallelism)
			_, err := hashMissionControlAccessToken(token)
			Expect(err).To(MatchError("invalid access token format"))
		})
	}
})
