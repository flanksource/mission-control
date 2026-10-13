package auth

import (
	gocontext "context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

func init() {
	adapter.ClerkVerifiedPeople = resolveClerkVerifiedPeople
}

// resolveClerkVerifiedPeople uses Backend API records, not configurable session email claims.
// Missing credentials, unavailable records and unknown verification states grant nothing.
func resolveClerkVerifiedPeople(ctx context.Context, emails []string) (map[string][]string, error) {
	key := os.Getenv("CLERK_SECRET_KEY")
	if key == "" || len(emails) == 0 {
		return nil, nil
	}

	requestContext, cancel := gocontext.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wanted := make(map[string]bool, len(emails))
	query := url.Values{"limit": {"100"}}
	for _, email := range emails {
		wanted[email] = true
		query.Add("email_address", email)
	}
	var externalIDs []string
	verifiedEmails := map[string][]string{}
	for offset := 0; ; offset += 100 {
		query.Set("offset", strconv.Itoa(offset))
		req, err := http.NewRequestWithContext(requestContext, http.MethodGet, "https://api.clerk.com/v1/users?"+query.Encode(), nil)
		if err != nil {
			return nil, nil
		}
		req.Header.Set("Authorization", "Bearer "+key)
		response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			ctx.Warnf("Clerk verified-email lookup unavailable")
			return nil, nil
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			ctx.Warnf("Clerk verified-email lookup returned HTTP %d", response.StatusCode)
			return nil, nil
		}

		var users []struct {
			ID             string `json:"id"`
			EmailAddresses []struct {
				Email        string `json:"email_address"`
				Verification struct {
					Status string `json:"status"`
				} `json:"verification"`
			} `json:"email_addresses"`
		}
		err = json.NewDecoder(response.Body).Decode(&users)
		response.Body.Close()
		if err != nil {
			ctx.Warnf("Clerk verified-email lookup returned an invalid response")
			return nil, nil
		}
		for _, user := range users {
			for _, address := range user.EmailAddresses {
				if user.ID != "" && wanted[address.Email] && address.Verification.Status == "verified" {
					externalIDs = append(externalIDs, user.ID)
					verifiedEmails[user.ID] = append(verifiedEmails[user.ID], address.Email)
				}
			}
		}
		if len(users) < 100 {
			break
		}
	}
	if len(externalIDs) == 0 {
		return nil, nil
	}

	var people []models.Person
	err := ctx.DB().Model(&models.Person{}).Select("id", "external_id").
		Where("external_id IN ? AND deleted_at IS NULL", externalIDs).
		Where("(type IS NULL OR type NOT IN ?)", []string{"agent", "federated", "access_token"}).Find(&people).Error
	if err != nil {
		ctx.Warnf("Clerk verified-email people lookup unavailable")
		return nil, nil
	}
	idsByEmail := map[string][]string{}
	for _, person := range people {
		for _, email := range verifiedEmails[person.ExternalID] {
			idsByEmail[email] = append(idsByEmail[email], person.ID.String())
		}
	}
	return idsByEmail, nil
}
