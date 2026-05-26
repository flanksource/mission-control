package auth

import (
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/models"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("GetOrCreateJWTToken", func() {
	ginkgo.BeforeEach(func() {
		FlushTokenCache()
		api.DefaultConfig.Postgrest.JWTSecret = "test-secret"
		api.DefaultConfig.Postgrest.DBRole = "postgrest_api"
	})

	ginkgo.It("adds expiry claims to internal PostgREST JWTs", func() {
		user := &models.Person{ID: uuid.New(), Name: "token-test"}

		tokenString, err := GetOrCreateJWTToken(DefaultContext, user, "session")
		Expect(err).NotTo(HaveOccurred())

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
			return []byte(api.DefaultConfig.Postgrest.JWTSecret), nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(token.Valid).To(BeTrue())

		claims := token.Claims.(jwt.MapClaims)
		Expect(claims["id"]).To(Equal(user.ID.String()))
		Expect(claims["role"]).To(Equal("postgrest_api"))
		Expect(claims["exp"]).NotTo(BeNil())
		Expect(claims["iat"]).NotTo(BeNil())

		exp, ok := claims["exp"].(float64)
		Expect(ok).To(BeTrue())
		Expect(time.Until(time.Unix(int64(exp), 0))).To(BeNumerically("~", jwtTokenLifetime, 5*time.Second))
	})
})
