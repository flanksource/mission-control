package shorturl

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

const (
	DefaultAliasLength = 6
	MaxAliasLength     = 50
	DefaultCacheTTL    = 24 * time.Hour
)

// Caches <alias, originalURL>
var urlCache = cache.New(DefaultCacheTTL, DefaultCacheTTL)

// Create creates a new shortened URL
func Create(ctx context.Context, targetURL string, expiresAt *time.Time) (*string, error) {
	if _, err := url.Parse(targetURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	alias, err := generateUniqueAlias(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate unique alias: %w", err)
	}

	shortURL := models.ShortURL{
		Alias:     alias,
		URL:       targetURL,
		ExpiresAt: expiresAt,
	}

	if err := ctx.DB().Create(&shortURL).Error; err != nil {
		return nil, fmt.Errorf("failed to save shortened URL: %w", err)
	}

	urlCache.Set(alias, targetURL, lo.If(shortURL.ExpiresAt != nil, time.Until(lo.FromPtr(shortURL.ExpiresAt))).Else(cache.NoExpiration))
	return &alias, nil
}

func Get(ctx context.Context, alias string) (string, error) {
	if cachedItem, found := urlCache.Get(alias); found {
		if targetURL, ok := cachedItem.(string); ok {
			return targetURL, nil
		}
	}

	var shortURL models.ShortURL
	if err := ctx.DB().Where("alias = ?", alias).Where("expires_at IS NULL OR expires_at > NOW()").First(&shortURL).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", api.Errorf(api.ENOTFOUND, "alias '%s' not found", alias)
		}
		return "", fmt.Errorf("failed to retrieve URL (alias: %s): %w", alias, err)
	}

	urlCache.Set(alias, shortURL.URL, lo.If(shortURL.ExpiresAt != nil, time.Until(lo.FromPtr(shortURL.ExpiresAt))).Else(cache.NoExpiration))
	return shortURL.URL, nil
}

// generateUniqueAlias generates a unique random alias
func generateUniqueAlias(ctx context.Context) (string, error) {
	const maxAttempts = 5

	for range maxAttempts {
		alias, err := generateRandomAlias(DefaultAliasLength)
		if err != nil {
			return "", err
		}

		var existing models.ShortURL
		if err := ctx.DB().Where("alias = ?", alias).Find(&existing).Error; existing.Alias == "" {
			return alias, nil
		} else if err != nil {
			return "", fmt.Errorf("failed to check for existing alias: %w", err)
		}
	}

	return "", fmt.Errorf("failed to generate unique alias after %d attempts", maxAttempts)
}

// generateRandomAlias generates a random base64 URL-safe string
func generateRandomAlias(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	encoded := base64.URLEncoding.EncodeToString(bytes)
	if len(encoded) > length {
		encoded = encoded[:length]
	}

	return encoded, nil
}

func CleanupExpired(ctx job.JobRuntime) error {
	result := ctx.DB().Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).
		Delete(&models.ShortURL{})

	if result.Error != nil {
		return fmt.Errorf("failed to cleanup expired URLs: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		ctx.History.SuccessCount = int(result.RowsAffected)
	}

	return nil
}
