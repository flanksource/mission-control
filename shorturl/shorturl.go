package shorturl

import (
	"fmt"
	"net/url"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/oklog/ulid/v2"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
)

const (
	DefaultCacheTTL  = 24 * time.Hour
	DefaultURLExpiry = 90 * 24 * time.Hour // 90 days
)

var urlCache = cache.New(DefaultCacheTTL, DefaultCacheTTL)

// Create creates a new shortened URL with default expiry
func Create(ctx context.Context, targetURL string) (*string, error) {
	defaultExpiry := ctx.Properties().Duration("shorturl.defaultExpiry", DefaultURLExpiry)
	expiryTime := time.Now().Add(defaultExpiry)
	return CreateWithExpiry(ctx, targetURL, &expiryTime)
}

// CreateWithExpiry creates a new shortened URL with custom expiry
func CreateWithExpiry(ctx context.Context, targetURL string, expiresAt *time.Time) (*string, error) {
	if _, err := url.Parse(targetURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	alias := ulid.Make().String()
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
			return "", dutyAPI.Errorf(dutyAPI.ENOTFOUND, "alias '%s' not found", alias)
		}
		return "", fmt.Errorf("failed to retrieve URL (alias: %s): %w", alias, err)
	}

	urlCache.Set(alias, shortURL.URL, lo.If(shortURL.ExpiresAt != nil, time.Until(lo.FromPtr(shortURL.ExpiresAt))).Else(cache.NoExpiration))
	return shortURL.URL, nil
}

func CleanupExpired(ctx job.JobRuntime) error {
	var expiredAliases []string
	if err := ctx.DB().Model(&models.ShortURL{}).
		Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).
		Pluck("alias", &expiredAliases).Error; err != nil {
		return fmt.Errorf("failed to fetch expired aliases: %w", err)
	}

	if len(expiredAliases) == 0 {
		return nil
	}

	result := ctx.DB().Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).
		Delete(&models.ShortURL{})

	if result.Error != nil {
		return fmt.Errorf("failed to cleanup expired URLs: %w", result.Error)
	}

	for _, alias := range expiredAliases {
		urlCache.Delete(alias)
	}

	if result.RowsAffected > 0 {
		ctx.History.SuccessCount = int(result.RowsAffected)
	}

	return nil
}

func FullShortURL(alias string, prefix ...string) (string, error) {
	return fullShortURL(api.FrontendURL, alias, prefix...)
}

func fullShortURL(host, alias string, prefix ...string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("failed to parse frontend URL: %w", err)
	}

	u = u.JoinPath(redirectPath)
	u = u.JoinPath(prefix...)

	query := u.Query()
	query.Set("alias", alias)
	u.RawQuery = query.Encode()
	return u.String(), nil
}
