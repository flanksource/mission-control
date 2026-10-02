package auth

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/kopper"
	"github.com/golang-jwt/jwt/v4"

	v1 "github.com/flanksource/incident-commander/api/v1"
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

	if invalid == nil && !vars.RLSEnabled(ctx) {
		ctx.Warnf("%s is off: users of external identity provider %s/%s can only be granted reads of whole types", vars.FlagRLSEnable, obj.Namespace, obj.Name)
	}

	if invalid != nil {
		return kopper.NotReady(kopper.ReasonInvalid, "%w", invalid)
	}
	return nil
}

func DeleteExternalIdentityProvider(ctx context.Context, id string) error {
	federation.Lock()
	removeFederatedProvider(id)
	changed := resolveFederatedConflicts(ctx, "")
	federation.Unlock()

	notifyProviderValidityChanged(changed)
	return nil
}

func DeleteStaleExternalIdentityProvider(ctx context.Context, newer *v1.ExternalIdentityProvider) error {
	federation.Lock()
	for id, provider := range federation.providers {
		if id != string(newer.GetUID()) && provider.namespace == newer.Namespace && provider.name == newer.Name {
			removeFederatedProvider(id)
		}
	}
	changed := resolveFederatedConflicts(ctx, string(newer.GetUID()))
	federation.Unlock()

	notifyProviderValidityChanged(changed)
	return nil
}

// resolveFederatedConflicts recomputes which providers are in effect.
// Of the providers that share an issuer, or a name across namespaces, the oldest wins,
// and on a tie the one whose namespace sorts first. Providers that are wrong on their own don't compete.
// It returns the providers, other than skip, whose validity changed. The caller holds the federation lock.
func resolveFederatedConflicts(ctx context.Context, skip string) []*federatedProvider {
	var changed []*federatedProvider
	for _, provider := range federation.providers {
		invalid := provider.ownInvalid
		if invalid == nil {
			for _, other := range federation.providers {
				if other.id == provider.id || other.ownInvalid != nil || !olderFederatedProvider(other, provider) {
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
		if provider.spec.Issuer != issuer {
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

func stringClaim(claims jwt.MapClaims, name string) string {
	if name == "" {
		return ""
	}
	value, _ := claims[name].(string)
	return value
}
