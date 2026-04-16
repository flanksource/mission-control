package scraper

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/api"
)

var knownBackendKeys = map[string]bool{
	"kubernetes":     true,
	"aws":            true,
	"azure":          true,
	"gcp":            true,
	"file":           true,
	"sql":            true,
	"http":           true,
	"trivy":          true,
	"terraform":      true,
	"githubActions":  true,
	"slack":          true,
	"kubernetesFile": true,
}

func BuildScraperInfo(ctx context.Context, scraperID uuid.UUID) (*api.ScraperInfo, error) {
	var scraper models.ConfigScraper
	if err := ctx.DB().Where("id = ?", scraperID).First(&scraper).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ctx.Oops().Wrapf(err, "scraper %s not found", scraperID)
		}
		return nil, ctx.Oops().Wrapf(err, "failed to query scraper %s", scraperID)
	}

	types := parseSpecTypes(scraper.Spec)
	if types == nil {
		types = []string{}
	}

	info := &api.ScraperInfo{
		ID:        scraper.ID.String(),
		Name:      scraper.Name,
		Namespace: scraper.Namespace,
		Source:    scraper.Source,
		CreatedAt: scraper.CreatedAt.Format(time.RFC3339),
		SpecHash:  specSHA256(scraper.Spec),
		Types:     types,
	}

	if scraper.Description != "" {
		info.Description = scraper.Description
	}

	if scraper.UpdatedAt != nil {
		info.UpdatedAt = scraper.UpdatedAt.Format(time.RFC3339)
	}

	if scraper.CreatedBy != nil {
		var person models.Person
		if err := ctx.DB().Where("id = ?", scraper.CreatedBy).First(&person).Error; err == nil {
			info.CreatedBy = person.GetName()
		}
	}

	source, err := query.GetGitOpsSource(ctx, scraperID)
	if err != nil {
		ctx.Logger.V(3).Infof("no gitops source for scraper %s: %v", scraperID, err)
	} else if source.Git.URL != "" {
		info.GitOps = &source
	}

	return info, nil
}

func parseSpecTypes(spec string) []string {
	if spec == "" {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(spec), &parsed); err != nil {
		return nil
	}

	var types []string
	for key := range parsed {
		if knownBackendKeys[key] {
			types = append(types, key)
		}
	}
	sort.Strings(types)
	return types
}

func specSHA256(spec string) string {
	if spec == "" {
		return ""
	}
	h := sha256.Sum256([]byte(spec))
	return fmt.Sprintf("%x", h)
}
