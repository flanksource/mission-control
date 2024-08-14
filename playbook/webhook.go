package playbook

import (
	"bytes"
	gocontext "context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	durationutils "github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/api"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"

	mcApi "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook/actions"
)

const svixWebhookSecretPrefix = "whsec_"

var jwksCache = cache.New(10*time.Minute, time.Hour)

func HandleWebhook(c echo.Context) error {
	ctx := c.Request().Context().(context.Context).WithUser(&models.Person{ID: utils.Deref(mcApi.SystemUserID)})

	var path = c.Param("webhook_path")
	playbook, err := db.FindPlaybookByWebhookPath(ctx, path)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	} else if playbook == nil {
		return c.JSON(http.StatusNotFound, dutyAPI.HTTPError{Err: "not found", Message: fmt.Sprintf("playbook(webhook_path=%s) not found", path)})
	}

	var spec v1.PlaybookSpec
	if err := json.Unmarshal(playbook.Spec, &spec); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{Err: err.Error(), Message: "playbook has an invalid spec"})
	}

	if err := authenticateWebhook(ctx, c.Request(), spec.On.Webhook.Authentication); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	var runRequest RunParams
	whr, err := actions.NewWebhookRequest(c)
	if err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("error parsing webhook request data: %w", err))
	}

	runRequest.ID = playbook.ID
	runRequest.Request = whr

	if _, err = Run(ctx, playbook, runRequest); err != nil {
		return dutyAPI.WriteError(c, fmt.Errorf("failed to save playbook run: %w", err))
	}

	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: "ok"})
}

func authenticateWebhook(ctx context.Context, r *http.Request, auth *v1.PlaybookEventWebhookAuth) error {
	if auth.Basic != nil {
		username, password, ok := r.BasicAuth()
		if !ok {
			return api.Errorf(api.EFORBIDDEN, "username and password required")
		}

		expectedUsername, err := ctx.GetEnvValueFromCache(auth.Basic.Username, ctx.GetNamespace())
		if err != nil {
			return err
		}

		expectedPassword, err := ctx.GetEnvValueFromCache(auth.Basic.Password, ctx.GetNamespace())
		if err != nil {
			return err
		}

		if subtle.ConstantTimeCompare([]byte(username), []byte(expectedUsername)) == 0 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 0 {
			return api.Errorf(api.EUNAUTHORIZED, "username/password did not match")
		}
	}

	if auth.Github != nil {
		sig := strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")

		token, err := ctx.GetEnvValueFromCache(auth.Github.Token, ctx.GetNamespace())
		if err != nil {
			return err
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(b))

		hash := hmac.New(sha256.New, []byte(token))
		if _, err := hash.Write(b); err != nil {
			return err
		}

		expectedHash := hex.EncodeToString(hash.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(expectedHash), []byte(sig)) == 0 {
			return api.Errorf(api.EUNAUTHORIZED, "invalid signature")
		}
	}

	if auth.JWT != nil {
		token := strings.TrimSpace(strings.Replace(r.Header.Get("Authorization"), "Bearer", "", 1))
		return validateJWT(ctx, auth.JWT.JWKSURI, token)
	}

	if auth.SVIX != nil {
		signingKey, err := ctx.GetEnvValueFromCache(auth.SVIX.Secret, ctx.GetNamespace())
		if err != nil {
			return err
		}

		signingKeyB64, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(signingKey, svixWebhookSecretPrefix))
		if err != nil {
			return err
		}

		verifier := svix{
			SigningKey: signingKeyB64,
		}

		var (
			msgID        = r.Header.Get("svix-id")
			msgSignature = r.Header.Get("svix-signature")
			msgTimestamp = r.Header.Get("svix-timestamp")
		)

		if msgID == "" || msgSignature == "" || msgTimestamp == "" {
			msgID = r.Header.Get("webhook-id")
			msgSignature = r.Header.Get("webhook-signature")
			msgTimestamp = r.Header.Get("webhook-timestamp")
			if msgID == "" || msgSignature == "" || msgTimestamp == "" {
				return api.Errorf(api.EINVALID, "missing svix headers")
			}
		}

		timestamp, err := verifier.parseTimestampHeader(msgTimestamp)
		if err != nil {
			return api.Errorf(api.EINVALID, "bad timestamp")
		}

		if auth.SVIX.TimestampTolerance != "" {
			tolerance, err := durationutils.ParseDuration(auth.SVIX.TimestampTolerance)
			if err != nil {
				return err
			}

			if err := verifier.verifyTimestamp(timestamp, time.Duration(tolerance)); err != nil {
				return err
			}
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(payload))

		computedSignature, err := verifier.Sign(msgID, timestamp, payload)
		if err != nil {
			return err
		}
		expectedSignature := []byte(strings.Split(computedSignature, ",")[1])

		passedSignatures := strings.Split(msgSignature, " ")
		for _, versionedSignature := range passedSignatures {
			sigParts := strings.Split(versionedSignature, ",")
			if len(sigParts) < 2 {
				continue
			}

			version := sigParts[0]
			signature := []byte(sigParts[1])

			if version != "v1" {
				continue
			}

			if hmac.Equal(signature, expectedSignature) {
				return nil
			}
		}

		return api.Errorf(api.EUNAUTHORIZED, "invalid signature")
	}

	return nil
}

type svix struct {
	SigningKey []byte
}

func (t *svix) parseTimestampHeader(timestampHeader string) (time.Time, error) {
	timeInt, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	timestamp := time.Unix(timeInt, 0)
	return timestamp, nil
}

func (t *svix) verifyTimestamp(timestamp time.Time, tolerance time.Duration) error {
	now := time.Now()

	if now.Sub(timestamp) > tolerance {
		return fmt.Errorf("timestamp is too old")
	}
	if timestamp.Unix() > now.Add(tolerance).Unix() {
		return fmt.Errorf("timestamp is too new")
	}

	return nil
}

func (t *svix) Sign(msgId string, timestamp time.Time, payload []byte) (string, error) {
	toSign := fmt.Sprintf("%s.%d.%s", msgId, timestamp.Unix(), payload)

	h := hmac.New(sha256.New, t.SigningKey)
	h.Write([]byte(toSign))
	sig := make([]byte, base64.StdEncoding.EncodedLen(h.Size()))
	base64.StdEncoding.Encode(sig, h.Sum(nil))
	return fmt.Sprintf("v1,%s", sig), nil
}

func validateJWT(ctx gocontext.Context, jwksURL, jwtB64 string) error {
	var jwks *keyfunc.JWKS
	if val, ok := jwksCache.Get(jwksURL); ok {
		jwks = val.(*keyfunc.JWKS)
	} else {
		options := keyfunc.Options{
			Ctx: ctx,
			RefreshErrorHandler: func(err error) {
				logger.Errorf("there was an error with the jwt.Keyfunc: %w", err)
			},
			JWKUseWhitelist:   []keyfunc.JWKUse{},
			RefreshInterval:   time.Hour,
			RefreshRateLimit:  time.Minute * 5,
			RefreshTimeout:    time.Second * 10,
			RefreshUnknownKID: true,
		}

		var err error
		jwks, err = keyfunc.Get(jwksURL, options)
		if err != nil {
			return fmt.Errorf("failed to create JWKS from resource at the given URL: %w", err)
		}
		defer jwks.EndBackground()

		jwksCache.SetDefault(jwksURL, jwks)
	}

	token, err := jwt.Parse(jwtB64, jwks.Keyfunc)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenUnverifiable) ||
			errors.Is(err, jwt.ErrTokenSignatureInvalid) ||
			errors.Is(err, jwt.ErrTokenExpired) ||
			errors.Is(err, jwt.ErrTokenMalformed) {
			return api.Errorf(api.EUNAUTHORIZED, "%v", err)
		}

		return fmt.Errorf("failed to parse the JWT: %w", err)
	}

	if !token.Valid {
		return api.Errorf(api.EUNAUTHORIZED, "the token is not valid.")
	}

	return nil
}
