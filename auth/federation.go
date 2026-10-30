package auth

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/kopper"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/cel-go/cel"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	slogecho "github.com/samber/slog-echo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
	"github.com/flanksource/incident-commander/vars"
)

// federatedClockSkew is tolerated when checking a token's exp, iat and nbf claims.
const federatedClockSkew = 30 * time.Second

// federatedKeyRetryBackoff limits outbound retries while a provider's keys are unavailable.
const federatedKeyRetryBackoff = 30 * time.Second

// federatedAuthError is a token that claims a registered issuer but fails verification.
type federatedAuthError struct {
	err error
}

func (e federatedAuthError) Error() string {
	return e.err.Error()
}

func federatedAuthErrorf(format string, args ...any) error {
	return federatedAuthError{err: fmt.Errorf(format, args...)}
}

type federatedProvider struct {
	id, namespace, name string
	created             time.Time
	spec                v1.ExternalIdentityProviderSpec
	maxTokenAge         time.Duration
	keys                *federatedKeys
	identity            v1.CompiledIdentity

	// ownIssuer is set when the issuer is one Mission Control's own authentication uses: the provider claims no tokens.
	ownIssuer bool

	// deleting is set while a deleted provider's people are retired. Its tokens are rejected.
	deleting bool

	// ownInvalid is why the provider is wrong on its own, regardless of other providers.
	ownInvalid error

	// invalid is why the provider isn't in effect: ownInvalid, or a conflict with an older provider.
	// Its tokens are rejected, as if it were disabled.
	invalid error
}

// federatedKeys lazily resolves and caches the provider's signing keys.
// It's shared across updates of a provider that don't change where the keys come from.
type federatedKeys struct {
	issuer, jwksURL string

	mu         sync.Mutex
	jwks       *keyfunc.JWKS
	loading    chan struct{}
	loadCancel gocontext.CancelFunc
	closed     bool
	lastErr    error
	retryAfter time.Time
}

func (k *federatedKeys) keyfunc(ctx context.Context) (jwt.Keyfunc, error) {
	for {
		k.mu.Lock()
		if k.closed {
			k.mu.Unlock()
			return nil, federatedAuthErrorf("external identity provider was removed or changed")
		}
		if k.jwks != nil {
			keyfunc := k.jwks.Keyfunc
			k.mu.Unlock()
			return keyfunc, nil
		}
		if k.lastErr != nil && time.Now().Before(k.retryAfter) {
			err := k.lastErr
			k.mu.Unlock()
			return nil, err
		}
		if k.loading != nil {
			loading := k.loading
			k.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-loading:
				continue
			}
		}

		loading := make(chan struct{})
		loadCtx, cancel := gocontext.WithCancel(gocontext.Background())
		k.loading = loading
		k.loadCancel = cancel
		k.mu.Unlock()

		go k.load(loadCtx, loading)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-loading:
			continue
		}
	}
}

// load performs one shared signing-key initialization and publishes its result.
func (k *federatedKeys) load(ctx gocontext.Context, loading chan struct{}) {
	defer func() {
		k.mu.Lock()
		k.loading = nil
		k.loadCancel = nil
		close(loading)
		k.mu.Unlock()
	}()

	jwksURL := k.jwksURL
	var err error
	if jwksURL == "" {
		jwksURL, err = discoverJWKSURL(ctx, k.issuer)
	}

	var jwks *keyfunc.JWKS
	if err == nil {
		jwks, err = newFederatedJWKS(ctx, jwksURL)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		if jwks != nil {
			jwks.EndBackground()
		}
		return
	}
	if err != nil {
		k.lastErr = err
		k.retryAfter = time.Now().Add(federatedKeyRetryBackoff)
		return
	}

	k.jwks = jwks
	k.lastErr = nil
	k.retryAfter = time.Time{}
}

// close stops refreshing the keys in the background.
func (k *federatedKeys) close() {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return
	}
	k.closed = true
	if k.loadCancel != nil {
		k.loadCancel()
	}
	jwks := k.jwks
	k.jwks = nil
	k.mu.Unlock()

	if jwks != nil {
		jwks.EndBackground()
	}
}

const maxKeyRedirects = 5

// keyFetchClient fetches OpenID configurations and signing keys.
// Every redirect must pass the same https rule as the configured URLs, so a
// redirect can't downgrade the fetch to plaintext http, where the response can be
// swapped for an attacker's keys. Plain http is only followed to a loopback host
// from plain http (local development), never from https.
var keyFetchClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// via holds the requests already made: the original one, and one per redirect followed
		if len(via) > maxKeyRedirects {
			return fmt.Errorf("stopped after %d redirects", maxKeyRedirects)
		}
		if req.URL.Scheme == "https" {
			return nil
		}
		if isLoopback(req.URL) && via[len(via)-1].URL.Scheme != "https" {
			return nil
		}
		return fmt.Errorf("refusing redirect from %s to %s: signing keys must be fetched over https", via[len(via)-1].URL, req.URL)
	},
}

func newFederatedJWKS(ctx gocontext.Context, jwksURL string) (*keyfunc.JWKS, error) {
	options := keyfunc.Options{
		Ctx: ctx,
		RefreshErrorHandler: func(err error) {
			logger.Errorf("There was an error refreshing external identity provider signing keys: %s", err)
		},
		RefreshInterval:   time.Hour,
		RefreshRateLimit:  5 * time.Minute,
		RefreshTimeout:    10 * time.Second,
		RefreshUnknownKID: true,
		Client:            keyFetchClient,
	}

	jwks, err := keyfunc.Get(jwksURL, options)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %q: %w", jwksURL, err)
	}
	return jwks, nil
}

func isLoopback(u *url.URL) bool {
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// discoverJWKSURL returns the jwks_uri from the issuer's OpenID configuration.
func discoverJWKSURL(ctx gocontext.Context, issuer string) (string, error) {
	configURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := keyFetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch %s: %w", configURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch %s: %s", configURL, resp.Status)
	}

	var config struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return "", fmt.Errorf("invalid OpenID configuration at %s: %w", configURL, err)
	} else if config.JWKSURI == "" {
		return "", fmt.Errorf("OpenID configuration at %s has no jwks_uri", configURL)
	}

	if err := v1.ValidateIssuerURL("jwks_uri", config.JWKSURI); err != nil {
		return "", fmt.Errorf("OpenID configuration at %s: %w", configURL, err)
	}

	return config.JWKSURI, nil
}

// federation holds the registered external identity providers, keyed by id.
// Mission Control runs as a single replica, so the reconciler keeps them in memory.
var federation = struct {
	sync.RWMutex
	providers map[string]*federatedProvider
}{providers: map[string]*federatedProvider{}}

// ProviderValidityChanged is called with a provider whose validity changed because of another provider,
// so its Ready condition can be updated.
var ProviderValidityChanged func(namespace, name string)

// PersistExternalIdentityProvider registers the provider as written.
// An invalid provider is registered too, and rejects its tokens with 401 until it becomes valid:
// there's no previous version to fall back to.
func PersistExternalIdentityProvider(ctx context.Context, obj *v1.ExternalIdentityProvider) error {
	provider := &federatedProvider{
		id:        string(obj.GetUID()),
		namespace: obj.Namespace,
		name:      obj.Name,
		created:   obj.CreationTimestamp.Time,
		spec:      obj.Spec,
	}

	if err := obj.Spec.Validate(); err != nil {
		provider.ownInvalid = ctx.Oops().Code(api.EINVALID).Wrapf(err, "invalid external identity provider")
	} else if provider.maxTokenAge, err = obj.Spec.GetMaxTokenAge(); err != nil {
		provider.ownInvalid = ctx.Oops().Code(api.EINVALID).Wrap(err)
	} else if provider.identity, err = obj.Spec.Identity.Compile(); err != nil {
		provider.ownInvalid = ctx.Oops().Code(api.EINVALID).Wrap(err)
	} else if isOwnIssuer(obj.Spec.Issuer) {
		provider.ownIssuer = true
		provider.ownInvalid = ctx.Oops().Code(api.EINVALID).Errorf("issuer %s is used by Mission Control's own authentication", obj.Spec.Issuer)
	}

	federation.Lock()
	if existing, ok := federation.providers[provider.id]; ok && existing.keys != nil &&
		existing.keys.issuer == provider.spec.Issuer && existing.keys.jwksURL == provider.spec.JWKSURL {
		provider.keys = existing.keys
	} else {
		if ok && existing.keys != nil {
			existing.keys.close()
		}
		provider.keys = &federatedKeys{issuer: provider.spec.Issuer, jwksURL: provider.spec.JWKSURL}
	}

	federation.providers[provider.id] = provider
	changed := resolveFederatedConflicts(ctx, provider.id)
	invalid := provider.invalid
	federation.Unlock()

	notifyProviderValidityChanged(changed)

	if invalid != nil {
		meta.RemoveStatusCondition(&obj.Status.Conditions, v1.ConditionAllowedTeams)
		return kopper.NotReady(kopper.ReasonInvalid, "%w", invalid)
	}

	// Removing a team from allowed, or removing identity.teams, removes the memberships at once
	var allowed []string
	if teams := obj.Spec.Identity.Teams; teams != nil {
		allowed = lo.Compact(teams.Allowed)
	}
	removed, err := db.RemoveFederatedMemberships(ctx, provider.name, allowed)
	if err != nil {
		return err
	}
	applyRemovedMemberships(ctx, removed)

	return setAllowedTeams(ctx, obj, allowed)
}

// setAllowedTeams sets the AllowedTeams condition of a provider with a team mapper: whether every allowed team exists,
// and a warning when the shipped admins team is allowed.
func setAllowedTeams(ctx context.Context, obj *v1.ExternalIdentityProvider, allowed []string) error {
	if len(allowed) == 0 {
		meta.RemoveStatusCondition(&obj.Status.Conditions, v1.ConditionAllowedTeams)
		return nil
	}

	var existing []string
	if err := ctx.DB().Model(&models.Team{}).Where("name IN ? AND deleted_at IS NULL", allowed).Pluck("name", &existing).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to find the allowed teams of external identity provider %s", obj.Name)
	}

	condition := metav1.Condition{
		Type:               v1.ConditionAllowedTeams,
		Status:             metav1.ConditionTrue,
		Reason:             "AllowedTeamsExist",
		ObservedGeneration: obj.GetGeneration(),
	}

	var messages []string
	if missing, _ := lo.Difference(allowed, existing); len(missing) > 0 {
		condition.Reason = "TeamNotFound"
		messages = append(messages, fmt.Sprintf("allowed teams %v don't exist", missing))
	}
	if slices.Contains(allowed, shipped.TeamAdmins) {
		condition.Reason = lo.Ternary(len(messages) > 0, condition.Reason, "AllowsAdmins")
		messages = append(messages, fmt.Sprintf("the %s team is allowed, so the application's claims decide who is a Mission Control admin", shipped.TeamAdmins))
	}
	if len(messages) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Message = strings.Join(messages, "; ")
	}

	meta.SetStatusCondition(&obj.Status.Conditions, condition)
	return nil
}

// isOwnIssuer reports whether the issuer is one Mission Control's own authentication uses:
// its embedded OIDC issuer, or the configured Clerk instance.
func isOwnIssuer(issuer string) bool {
	issuer = strings.TrimRight(issuer, "/")
	if OIDCEnabled && issuer == OIDCIssuerURL() {
		return true
	}

	if vars.AuthMode == Clerk && ClerkJwksUrl != "" {
		// A Clerk instance's tokens are issued by the origin its JWKS is served from
		if u, err := url.Parse(ClerkJwksUrl); err == nil && issuer == u.Scheme+"://"+u.Host {
			return true
		}
	}
	return false
}

// applyRemovedMemberships updates the policy for team memberships removed from the database.
func applyRemovedMemberships(ctx context.Context, removed []dbModels.TeamMember) {
	for _, m := range removed {
		if err := applyMembership(m.PersonID, m.TeamID, false); err != nil {
			ctx.Errorf("failed to remove person %s from team %s: %v", m.PersonID, m.TeamID, err)
		}
	}
}

// DeleteExternalIdentityProvider unregisters the provider. When it held its name, its people are retired first,
// so a provider that takes the name next creates new people. Its tokens are rejected meanwhile.
func DeleteExternalIdentityProvider(ctx context.Context, id string) error {
	federation.Lock()
	provider, ok := federation.providers[id]
	holdsName := ok && !slices.ContainsFunc(lo.Values(federation.providers), func(other *federatedProvider) bool {
		return other.id != id && other.name == provider.name && olderFederatedProvider(other, provider)
	})
	if ok {
		provider.deleting = true
	}
	federation.Unlock()

	if holdsName {
		removed, err := db.RetireFederatedPeople(ctx, provider.name)
		if err != nil {
			return err
		}
		applyRemovedMemberships(ctx, removed)
	}

	federation.Lock()
	removeFederatedProvider(id)
	changed := resolveFederatedConflicts(ctx, "")
	federation.Unlock()

	notifyProviderValidityChanged(changed)
	return nil
}

func DeleteStaleExternalIdentityProvider(ctx context.Context, newer *v1.ExternalIdentityProvider) error {
	federation.RLock()
	var stale []string
	for id, provider := range federation.providers {
		if id != string(newer.GetUID()) && provider.namespace == newer.Namespace && provider.name == newer.Name {
			stale = append(stale, id)
		}
	}
	federation.RUnlock()

	for _, id := range stale {
		if err := DeleteExternalIdentityProvider(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// resolveFederatedConflicts recomputes which providers are in effect.
// Of the providers that share an issuer, or a name across namespaces, the oldest wins,
// and on a tie the one whose namespace sorts first. A provider holds its name and issuer while it exists, valid or not.
// It returns the providers, other than skip, whose validity changed. The caller holds the federation lock.
func resolveFederatedConflicts(ctx context.Context, skip string) []*federatedProvider {
	var changed []*federatedProvider
	for _, provider := range federation.providers {
		invalid := provider.ownInvalid
		if invalid == nil {
			for _, other := range federation.providers {
				if other.id == provider.id || !olderFederatedProvider(other, provider) {
					continue
				}
				if other.spec.Issuer == provider.spec.Issuer {
					invalid = ctx.Oops().Code(api.ECONFLICT).Errorf("issuer %s is already used by the older provider %s/%s", provider.spec.Issuer, other.namespace, other.name)
					break
				} else if other.name == provider.name && other.namespace != provider.namespace {
					// RoleBindings refer to providers by name, so it must be unique across namespaces
					invalid = ctx.Oops().Code(api.ECONFLICT).Errorf("provider name %q is already used by the older provider in namespace %s", provider.name, other.namespace)
					break
				}
			}
		}

		if (invalid == nil) != (provider.invalid == nil) && provider.id != skip {
			changed = append(changed, provider)
		}
		provider.invalid = invalid
	}
	return changed
}

// olderFederatedProvider reports whether a was created before b, breaking ties by namespace, name and id.
func olderFederatedProvider(a, b *federatedProvider) bool {
	if !a.created.Equal(b.created) {
		return a.created.Before(b.created)
	}
	if a.namespace != b.namespace {
		return a.namespace < b.namespace
	}
	if a.name != b.name {
		return a.name < b.name
	}
	return a.id < b.id
}

func notifyProviderValidityChanged(providers []*federatedProvider) {
	if ProviderValidityChanged == nil {
		return
	}
	for _, provider := range providers {
		ProviderValidityChanged(provider.namespace, provider.name)
	}
}

// removeFederatedProvider unregisters a provider and stops refreshing its keys. The caller holds the federation lock.
func removeFederatedProvider(id string) {
	if provider, ok := federation.providers[id]; ok && provider.keys != nil {
		provider.keys.close()
	}
	delete(federation.providers, id)
}

// findFederatedProvider returns a snapshot of the provider with the issuer, preferring a valid one to an invalid one.
func findFederatedProvider(issuer string) *federatedProvider {
	federation.RLock()
	defer federation.RUnlock()

	var found *federatedProvider
	for _, provider := range federation.providers {
		if provider.spec.Issuer != issuer || provider.ownIssuer {
			continue
		}
		snapshot := *provider
		if provider.invalid == nil {
			return &snapshot
		}
		found = &snapshot
	}
	return found
}

// verify checks the token's signature, issuer, audience and lifetime, and returns its claims.
func (p *federatedProvider) verify(ctx context.Context, token string) (jwt.MapClaims, error) {
	if p.invalid != nil {
		return nil, federatedAuthErrorf("external identity provider %s is invalid", p.name)
	}

	if p.spec.Disabled {
		return nil, federatedAuthErrorf("external identity provider %s is disabled", p.name)
	}

	if p.deleting {
		return nil, federatedAuthErrorf("external identity provider %s is being deleted", p.name)
	}

	keyfunc, err := p.keys.keyfunc(ctx)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to load signing keys for external identity provider %s", p.name)
	}

	// Registered claims are checked below, with clock skew and required exp and iat
	parser := jwt.NewParser(jwt.WithValidMethods(p.spec.GetAlgorithms()), jwt.WithoutClaimsValidation())

	claims := jwt.MapClaims{}
	if parsed, err := parser.ParseWithClaims(token, claims, keyfunc); err != nil {
		return nil, federatedAuthErrorf("invalid token: %v", err)
	} else if !parsed.Valid {
		return nil, federatedAuthErrorf("invalid token")
	}

	if iss, _ := claims["iss"].(string); iss != p.spec.Issuer {
		return nil, federatedAuthErrorf("token issuer %q does not match %q", iss, p.spec.Issuer)
	}

	if !claims.VerifyAudience(p.spec.Audience, true) {
		return nil, federatedAuthErrorf("token audience does not include %q", p.spec.Audience)
	}

	now := time.Now()
	exp, err := timeClaim(claims, "exp", true)
	if err != nil {
		return nil, err
	}
	iat, err := timeClaim(claims, "iat", true)
	if err != nil {
		return nil, err
	}
	nbf, err := timeClaim(claims, "nbf", false)
	if err != nil {
		return nil, err
	}

	if now.After(exp.Add(federatedClockSkew)) {
		return nil, federatedAuthErrorf("token has expired")
	}
	if iat.After(now.Add(federatedClockSkew)) {
		return nil, federatedAuthErrorf("token is issued in the future")
	}
	if !nbf.IsZero() && nbf.After(now.Add(federatedClockSkew)) {
		return nil, federatedAuthErrorf("token is not valid yet")
	}
	if lifetime := exp.Sub(iat); lifetime > p.maxTokenAge {
		return nil, federatedAuthErrorf("token lifetime %s exceeds the maximum of %s", lifetime, p.maxTokenAge)
	}

	return claims, nil
}

func timeClaim(claims jwt.MapClaims, name string, required bool) (time.Time, error) {
	value, ok := claims[name]
	if !ok {
		if required {
			return time.Time{}, federatedAuthErrorf("token has no %s claim", name)
		}
		return time.Time{}, nil
	}

	switch v := value.(type) {
	case float64:
		return time.Unix(int64(v), 0), nil
	case json.Number:
		seconds, err := v.Int64()
		if err != nil {
			return time.Time{}, federatedAuthErrorf("invalid %s claim: %v", name, err)
		}
		return time.Unix(seconds, 0), nil
	default:
		return time.Time{}, federatedAuthErrorf("invalid %s claim", name)
	}
}

// evalString evaluates an identity expression that returns a string.
func evalString(program cel.Program, claims jwt.MapClaims) (string, error) {
	out, _, err := program.Eval(map[string]any{"claims": map[string]any(claims)})
	if err != nil {
		return "", err
	}
	value, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("returned %v, not a string", out.Value())
	}
	return value, nil
}

// optionalString evaluates an optional identity expression. It returns fallback when the expression isn't set,
// and nil, keeping the stored value, when it fails.
func optionalString(ctx context.Context, field string, program cel.Program, claims jwt.MapClaims, fallback string) *string {
	if program == nil {
		return &fallback
	}
	value, err := evalString(program, claims)
	if err != nil {
		ctx.Logger.V(3).Infof("identity.%s of token failed, keeping the stored value: %v", field, err)
		return nil
	}
	return &value
}

// mapTeams runs the provider's team mapper on the claims and returns the teams it may add the person to.
func (p *federatedProvider) mapTeams(ctx context.Context, claims jwt.MapClaims) ([]string, error) {
	out, _, err := p.identity.Mapper.Eval(map[string]any{"claims": map[string]any(claims)})
	if err != nil {
		return nil, err
	}

	values, err := out.ConvertToNative(reflect.TypeOf([]string{}))
	if err != nil {
		return nil, fmt.Errorf("returned %v, not a list of strings", out.Value())
	}

	var teams []string
	for _, team := range lo.Uniq(lo.Compact(values.([]string))) {
		if slices.Contains(p.spec.Identity.Teams.Allowed, team) {
			teams = append(teams, team)
		} else {
			ctx.Logger.Warnf("team mapper of external identity provider %s returned team %q, which isn't allowed", p.name, team)
		}
	}
	return teams, nil
}

// authenticateFederatedToken authenticates a bearer JWT issued by a registered external identity provider.
//
// matched is false when the token isn't from a registered provider, so other authenticators can try it.
// A token from a registered provider that fails verification returns a federatedAuthError.
func authenticateFederatedToken(c echo.Context, token string) (matched bool, err error) {
	if strings.Count(token, ".") != 2 {
		return false, nil
	}

	unverified, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return false, nil
	}

	issuer, _ := unverified.Claims.(jwt.MapClaims)["iss"].(string)
	provider := findFederatedProvider(issuer)
	if provider == nil {
		return false, nil
	}

	ctx := c.Request().Context().(context.Context)
	claims, err := provider.verify(ctx, token)
	if err != nil {
		return true, err
	}

	username, err := evalString(provider.identity.Username, claims)
	if err != nil || username == "" {
		return true, federatedAuthErrorf("identity.username didn't return a non-empty string: %v", lo.CoalesceOrEmpty[any](err, "empty"))
	} else if strings.Contains(username, "*") {
		return true, federatedAuthErrorf("username %q must not contain '*'", username)
	}

	name := optionalString(ctx, "name", provider.identity.Name, claims, username)
	email := optionalString(ctx, "email", provider.identity.Email, claims, "")
	person, created, err := db.GetOrCreateFederatedPerson(ctx, provider.name, username, name, email)
	if err != nil {
		return true, err
	}

	// The bindings that name the person, deny bindings included, apply from their first request
	if created {
		if err := adapter.ReloadPolicy(); err != nil {
			return true, ctx.Oops().Wrapf(err, "failed to apply the role bindings of %s", person.ExternalID)
		}
	}

	ctx = ctx.WithUser(person)
	if provider.identity.Mapper != nil {
		lock, _ := teamMapperLocks.LoadOrStore(person.ID.String(), &sync.Mutex{})
		lock.(*sync.Mutex).Lock()
		defer lock.(*sync.Mutex).Unlock()

		teams, err := provider.mapTeams(ctx, claims)
		if err != nil {
			ctx.Logger.Warnf("team mapper of external identity provider %s failed for %s: %v", provider.name, person.ExternalID, err)
			return true, federatedAuthErrorf("team mapping failed: %v", err)
		}
		if err := syncMappedTeams(ctx, person.ID, db.FederatedTeamSource(provider.name), teams); err != nil {
			return true, ctx.Oops().Wrapf(err, "failed to sync the teams of %s", person.ExternalID)
		}
	}

	postgrestToken, err := GetOrCreateJWTToken(ctx, person, "federated:"+provider.name)
	if err != nil {
		return true, ctx.Oops().Wrapf(err, "failed to create token for %s", person.ExternalID)
	}
	c.Request().Header.Set(echo.HeaderAuthorization, "Bearer "+postgrestToken)

	AddLoginContext(c, person)
	slogecho.AddCustomAttributes(c, slog.String("auth.provider", provider.name))

	c.SetRequest(c.Request().WithContext(ctx))
	return true, nil
}

// federatedSession serves the request when it carries a bearer token from a registered external identity provider.
// handled is false when it doesn't, and the request must be authenticated by other means.
func federatedSession(c echo.Context, next echo.HandlerFunc) (handled bool, err error) {
	token, ok := extractBearerAuthToken(c.Request().Header)
	if !ok {
		return false, nil
	}

	matched, err := authenticateFederatedToken(c, token)
	if !matched {
		return false, nil
	}

	if err != nil {
		ctx := c.Request().Context().(context.Context)
		ctx.GetSpan().RecordError(err)

		var authErr federatedAuthError
		if errors.As(err, &authErr) {
			ctx.Logger.V(3).Infof("rejected federated token: %v", err)
			setWWWAuthenticate(c)
			if c.Response().Header().Get("WWW-Authenticate") == "" {
				c.Response().Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			}
			return true, c.String(http.StatusUnauthorized, "Unauthorized: "+authErr.Error())
		}

		ctx.Errorf("failed to authenticate federated token: %v", err)
		return true, c.String(http.StatusInternalServerError, "internal error")
	}

	return true, next(c)
}

func withFederatedAuth(fallback echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		fallbackHandler := fallback(next)
		return func(c echo.Context) error {
			if canSkipAuth(c) || (vars.AuthMode == Basic && localhostOnly && !isLocalhostRequest(c)) {
				return fallbackHandler(c)
			}
			if handled, err := federatedSession(c, next); handled {
				return err
			}
			return fallbackHandler(c)
		}
	}
}
