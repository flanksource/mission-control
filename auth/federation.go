package auth

import (
	"sync"
	"time"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/kopper"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/vars"
)

type federatedProvider struct {
	id, namespace, name string
	created             time.Time
	spec                v1.ExternalIdentityProviderSpec
	maxTokenAge         time.Duration

	// ownInvalid is why the provider is wrong on its own, regardless of other providers.
	ownInvalid error

	// invalid is why the provider isn't in effect: ownInvalid, or a conflict with an older provider.
	// Its tokens are rejected, as if it were disabled.
	invalid error
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

// removeFederatedProvider unregisters a provider. The caller holds the federation lock.
func removeFederatedProvider(id string) {
	delete(federation.providers, id)
}
