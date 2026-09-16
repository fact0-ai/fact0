package auth

import (
	"context"
	"sync"
	"time"

	"github.com/fact0-ai/fact0/internal/audit"
)

type cacheEntry struct {
	key    *audit.APIKey
	expiry time.Time
}

// CachedKeyStore wraps an audit.KeyStore with an in-process TTL cache keyed
// by key ID. Revoked keys may remain visible until the entry expires.
type CachedKeyStore struct {
	inner audit.KeyStore
	ttl   time.Duration
	mu    sync.RWMutex
	items map[string]cacheEntry
}

// NewCachedKeyStore constructs a CachedKeyStore.
func NewCachedKeyStore(inner audit.KeyStore, ttl time.Duration) *CachedKeyStore {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &CachedKeyStore{
		inner: inner,
		ttl:   ttl,
		items: make(map[string]cacheEntry),
	}
}

// GetKeyByID implements audit.KeyStore.
func (c *CachedKeyStore) GetKeyByID(ctx context.Context, id string) (*audit.APIKey, error) {
	now := time.Now()
	c.mu.RLock()
	if ent, ok := c.items[id]; ok && now.Before(ent.expiry) {
		k := ent.key
		c.mu.RUnlock()
		return k, nil
	}
	c.mu.RUnlock()

	k, err := c.inner.GetKeyByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.items[id] = cacheEntry{key: k, expiry: now.Add(c.ttl)}
	c.mu.Unlock()
	return k, nil
}

// CreateKey implements audit.KeyStore.
func (c *CachedKeyStore) CreateKey(ctx context.Context, k *audit.APIKey) error {
	return c.inner.CreateKey(ctx, k)
}

// ListKeys implements audit.KeyStore.
func (c *CachedKeyStore) ListKeys(ctx context.Context, tenantID string) ([]*audit.APIKey, error) {
	return c.inner.ListKeys(ctx, tenantID)
}

// RevokeKey implements audit.KeyStore.
func (c *CachedKeyStore) RevokeKey(ctx context.Context, tenantID, id string) error {
	if err := c.inner.RevokeKey(ctx, tenantID, id); err != nil {
		return err
	}
	c.invalidateTenant(ctx, tenantID)
	return nil
}

// RevokeAllKeys implements audit.KeyStore.
func (c *CachedKeyStore) RevokeAllKeys(ctx context.Context, tenantID string) error {
	if err := c.inner.RevokeAllKeys(ctx, tenantID); err != nil {
		return err
	}
	c.invalidateTenant(ctx, tenantID)
	return nil
}

func (c *CachedKeyStore) invalidateTenant(ctx context.Context, tenantID string) {
	keys, err := c.inner.ListKeys(ctx, tenantID)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.items, k.ID)
	}
}
