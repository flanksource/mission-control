package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/commons/collections"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/plugin/registry"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	changeTypePluginInvocation = "InvokePlugin"
	defaultDedupeWindow        = time.Hour
)

var initChangeCacheOnce sync.Once

type invocationChangeInput struct {
	Entry       registry.Entry
	Operation   string
	ConfigID    uuid.UUID
	User        models.Person
	Source      string
	Method      string
	ParamsHash  string
	Error       string
	RequestBody []byte
	QueryParams url.Values
}

func recordPluginInvocation(ctx dutyContext.Context, entry *registry.Entry, op string, configID uuid.UUID, source, method, paramsHash, errorMessage string, req *http.Request, requestBody []byte) {
	if !pluginInvocationAudited(entry, op) {
		return
	}

	user := models.Person{}
	if ctx.User() != nil {
		user = *ctx.User()
	}

	if err := recordInvocationChange(ctx, invocationChangeInput{
		Entry:       *entry,
		Operation:   op,
		ConfigID:    configID,
		User:        user,
		Source:      source,
		Method:      method,
		ParamsHash:  paramsHash,
		Error:       errorMessage,
		RequestBody: requestBody,
		QueryParams: requestQueryParams(req),
	}); err != nil {
		ctx.Logger.Warnf("record plugin invocation config change: %v", err)
	}
}

func pluginInvocationAudited(entry *registry.Entry, op string) bool {
	if entry == nil || len(entry.Spec.Audit) == 0 {
		return false
	}

	matches, _ := collections.MatchItem(op, entry.Spec.Audit...)
	return matches
}

func recordInvocationChange(ctx dutyContext.Context, in invocationChangeInput) error {
	change, err := buildInvocationConfigChange(ctx, in)
	if err != nil {
		return err
	}

	window := ctx.Properties().Duration("changes.dedup.window", defaultDedupeWindow)
	initChangeCacheOnce.Do(func() {
		if err := models.InitChangeFingerprintCache(ctx.DB(), window); err != nil {
			ctx.Logger.Warnf("init config change fingerprint cache: %v", err)
		}
	})

	nonDuped, deduped := models.DedupConfigChanges(window, []*models.ConfigChange{change})
	for _, c := range nonDuped {
		if err := ctx.DB().Create(c).Error; err != nil {
			return ctx.Oops().Wrapf(err, "record plugin invocation config change")
		}
	}
	for _, d := range deduped {
		if err := updateDedupedChange(ctx, d).Error; err != nil {
			return ctx.Oops().With("change_id", d.Change.ID).Wrapf(err, "update deduped plugin invocation config change")
		}
	}

	return nil
}

func buildInvocationConfigChange(ctx dutyContext.Context, in invocationChangeInput) (*models.ConfigChange, error) {
	if in.ParamsHash == "" {
		in.ParamsHash = hashBytes(nil)
	}
	detailsJSON, err := json.Marshal(invocationDetails(in))
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "marshal plugin invocation details")
	}

	createdAt := time.Now()
	fingerprint := invocationFingerprint(in.Entry.ID.String(), in.Operation, invocationUserID(in.User), in.ParamsHash)
	change := &models.ConfigChange{
		ID:          uuid.New().String(),
		ConfigID:    in.ConfigID.String(),
		ChangeType:  changeTypePluginInvocation,
		Severity:    models.SeverityInfo,
		Source:      invocationPluginSource(in.Entry),
		Summary:     invocationSummary(in),
		Fingerprint: &fingerprint,
		Details:     types.JSON(detailsJSON),
		CreatedAt:   &createdAt,
		Count:       1,
		IsPushed:    false,
	}
	if in.User.ID != uuid.Nil {
		change.CreatedBy = &in.User.ID
	}
	return change, nil
}

func updateDedupedChange(ctx dutyContext.Context, dedup models.ConfigChangeUpdate) *gorm.DB {
	change := dedup.Change
	update := map[string]any{
		"change_type":         change.ChangeType,
		"count":               gorm.Expr("count + ?", dedup.CountIncrement),
		"created_at":          change.CreatedAt,
		"created_by":          change.CreatedBy,
		"details":             change.Details,
		"diff":                change.Diff,
		"external_change_id":  change.ExternalChangeID,
		"external_created_by": change.ExternalCreatedBy,
		"fingerprint":         change.Fingerprint,
		"inserted_at":         time.Now(),
		"is_pushed":           false,
		"severity":            change.Severity,
		"source":              change.Source,
		"summary":             change.Summary,
	}
	if change.Patches != "" {
		update["patches"] = change.Patches
	}
	return ctx.DB().Model(&models.ConfigChange{}).Where("id = ?", change.ID).UpdateColumns(update)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func httpParamsHash(method string, values url.Values) string {
	return hashBytes([]byte(strings.ToUpper(method) + "\n" + normalizedQueryParams(values).Encode()))
}

func normalizedQueryParams(values url.Values) url.Values {
	copy := url.Values{}
	for key, vals := range values {
		if strings.EqualFold(key, "config_id") {
			continue
		}
		sorted := append([]string(nil), vals...)
		sort.Strings(sorted)
		copy[key] = sorted
	}
	return copy
}

func invocationFingerprint(pluginID, operation, userID, paramsHash string) string {
	return hashBytes([]byte(strings.Join([]string{pluginID, operation, userID, paramsHash}, "\x00")))
}

func invocationDetails(in invocationChangeInput) map[string]any {
	pluginName := in.Entry.Name
	if in.Entry.Manifest != nil && in.Entry.Manifest.Name != "" {
		pluginName = in.Entry.Manifest.Name
	}

	details := map[string]any{
		"plugin": map[string]any{
			"id":        in.Entry.ID.String(),
			"name":      pluginName,
			"namespace": in.Entry.Namespace,
		},
		"operation": in.Operation,
		"source":    in.Source,
		"method":    in.Method,
	}
	if in.Error != "" {
		details["error"] = in.Error
	}

	request := map[string]any{}
	if in.Source == "http" && len(in.QueryParams) > 0 {
		request["queryParam"] = in.QueryParams
	} else if in.Source == "grpc" && len(in.RequestBody) > 0 {
		request["body"] = requestBodyJSON(in.RequestBody)
	}
	if len(request) > 0 {
		details["request"] = request
	}

	return details
}

func invocationUserID(user models.Person) string {
	if user.ID == uuid.Nil {
		return ""
	}
	return user.ID.String()
}

func invocationSummary(in invocationChangeInput) string {
	return invocationPluginName(in.Entry) + "." + in.Operation
}

func invocationPluginName(entry registry.Entry) string {
	if entry.Manifest != nil && entry.Manifest.Name != "" {
		return entry.Manifest.Name
	}
	return entry.Name
}

func invocationPluginSource(entry registry.Entry) string {
	parts := []string{"mission-control", "plugin"}
	if entry.Namespace != "" {
		parts = append(parts, entry.Namespace)
	}
	parts = append(parts, entry.Name)
	return strings.Join(parts, "/")
}

func requestQueryParams(req *http.Request) url.Values {
	if req == nil || req.URL == nil {
		return url.Values{}
	}
	return normalizedQueryParams(req.URL.Query())
}

func requestBodyJSON(body []byte) any {
	if len(body) == 0 {
		return map[string]any{}
	}

	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		return string(body)
	}
	return out
}
