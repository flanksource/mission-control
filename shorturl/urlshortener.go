package shorturl

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

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

var urlCache *cache.Cache

func init() {
	urlCache = cache.New(DefaultCacheTTL, DefaultCacheTTL)
}

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

	urlCache.Set(alias, shortURL, lo.If(shortURL.ExpiresAt != nil, time.Until(*shortURL.ExpiresAt)).Else(cache.NoExpiration))
	return &alias, nil
}

func Get(ctx context.Context, alias string) (*models.ShortURL, error) {
	if cachedItem, found := urlCache.Get(alias); found {
		if shortURL, ok := cachedItem.(models.ShortURL); ok {
			return &shortURL, nil
		}
	}

	var shortURL models.ShortURL
	if err := ctx.DB().Where("alias = ?", alias).Where("expires_at IS NULL OR expires_at > NOW()").First(&shortURL).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("alias '%s' not found", alias)
		}
		return nil, fmt.Errorf("failed to retrieve URL (alias: %s): %w", alias, err)
	}

	urlCache.Set(alias, shortURL, lo.If(shortURL.ExpiresAt != nil, time.Until(*shortURL.ExpiresAt)).Else(cache.NoExpiration))
	return &shortURL, nil
}

// generateUniqueAlias generates a unique random alias
func generateUniqueAlias(ctx context.Context) (string, error) {
	const maxAttempts = 10

	for range maxAttempts {
		alias := generateRandomAlias(DefaultAliasLength)

		var existing models.ShortURL
		err := ctx.DB().Where("alias = ?", alias).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return alias, nil
		} else if err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("failed to generate unique alias after %d attempts", maxAttempts)
}

// generateRandomAlias generates a random base64 URL-safe string
func generateRandomAlias(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())[len(fmt.Sprintf("%d", time.Now().UnixNano()))-length:]
	}

	encoded := base64.URLEncoding.EncodeToString(bytes)
	if len(encoded) > length {
		encoded = encoded[:length]
	}

	return encoded
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
