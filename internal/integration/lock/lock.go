// Package lock ports the locks Shlink takes through symfony/lock: a LockFactory over a FlockStore in data/locks, or
// over a RedisStore wrapped in shlink-common's NamespacedStore when REDIS_SERVERS is set (config/autoload/
// locks.global.php).
package lock

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/integration/realtime"
)

// DefaultTTL is LockFactory::createLock()'s default TTL.
const DefaultTTL = 300 * time.Second

// errConflicted ports LockConflictedException: the lock is held by someone else.
var errConflicted = errors.New("lock conflicted")

// store ports the PersistingStoreInterface operations the factory uses.
type store interface {
	// save takes the lock without waiting, or returns errConflicted.
	save(ctx context.Context, k *key) error
	// waitAndSave blocks until the lock is taken (BlockingStoreInterface). Stores that do not block return false.
	waitAndSave(ctx context.Context, k *key) (bool, error)
	putOffExpiration(ctx context.Context, k *key, ttl time.Duration) error
	delete(ctx context.Context, k *key) error
}

// key ports Symfony's Key: the resource plus the state stores attach to it.
type key struct {
	resource string
	token    string
	release  func()
}

// Factory ports LockFactory.
type Factory struct {
	store store
}

// NewFlockFactory returns a factory over a FlockStore in dir.
func NewFlockFactory(dir string) *Factory {
	return &Factory{store: &flockStore{dir: dir}}
}

// FromEnv ports the lock_store alias: Redis, namespaced with CACHE_NAMESPACE, when REDIS_SERVERS exists in the env,
// otherwise a FlockStore over dir.
func FromEnv(dir string) (*Factory, error) {
	if !config.RedisServers.ExistsInEnv() {
		return NewFlockFactory(dir), nil
	}
	client, err := realtime.NewRedisClient(realtime.RedisOptionsFromEnv())
	if err != nil {
		return nil, err
	}
	return &Factory{store: &redisStore{client: client, namespace: config.CacheNamespace.Load().StringOrNil()}}, nil
}

// Lock ports Symfony's Lock for one resource.
type Lock struct {
	store store
	key   *key
	ttl   time.Duration
}

// CreateLock ports LockFactory::createLock(). A zero ttl means no expiration refresh.
func (f *Factory) CreateLock(resource string, ttl time.Duration) *Lock {
	return &Lock{store: f.store, key: &key{resource: resource}, ttl: ttl}
}

// Acquire ports Lock::acquire(). Without blocking it returns false when the lock is held elsewhere; blocking, it
// waits until it is free (or ctx is done), retrying every 100ms ± 10ms when the store cannot block.
func (l *Lock) Acquire(ctx context.Context, blocking bool) (bool, error) {
	if !blocking {
		if err := l.store.save(ctx, l.key); errors.Is(err, errConflicted) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	} else if ok, err := l.store.waitAndSave(ctx, l.key); err != nil {
		return false, err
	} else if !ok {
		for {
			err := l.store.save(ctx, l.key)
			if err == nil {
				break
			}
			if !errors.Is(err, errConflicted) {
				return false, err
			}
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(time.Duration(100+rand.IntN(21)-10) * time.Millisecond):
			}
		}
	}
	if l.ttl > 0 {
		if err := l.store.putOffExpiration(ctx, l.key, l.ttl); err != nil {
			_ = l.store.delete(ctx, l.key)
			return false, err
		}
	}
	return true, nil
}

// Release ports Lock::release().
func (l *Lock) Release(ctx context.Context) error {
	return l.store.delete(ctx, l.key)
}

// processLocks serializes goroutines of this process on a resource before they reach the store, so a blocking wait
// parks a goroutine instead of an OS thread and honors ctx. Entries are dropped when nobody holds or waits for them.
type processLocks struct {
	mu      sync.Mutex
	entries map[string]*processLock
}

type processLock struct {
	ch   chan struct{}
	refs int
}

func (p *processLocks) entry(resource string) *processLock {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string]*processLock{}
	}
	e, ok := p.entries[resource]
	if !ok {
		e = &processLock{ch: make(chan struct{}, 1)}
		p.entries[resource] = e
	}
	e.refs++
	return e
}

func (p *processLocks) drop(resource string, e *processLock) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e.refs--; e.refs == 0 {
		delete(p.entries, resource)
	}
}

// lock takes the process lock of resource, waiting when blocking. It returns nil when not blocking and the resource
// is taken.
func (p *processLocks) lock(ctx context.Context, resource string, blocking bool) (func(), error) {
	e := p.entry(resource)
	if blocking {
		select {
		case e.ch <- struct{}{}:
		case <-ctx.Done():
			p.drop(resource, e)
			return nil, ctx.Err()
		}
	} else {
		select {
		case e.ch <- struct{}{}:
		default:
			p.drop(resource, e)
			return nil, nil
		}
	}
	return func() {
		<-e.ch
		p.drop(resource, e)
	}, nil
}

// AcquireBlocking creates a lock with ttl and waits until it is held; it returns the release function.
func (f *Factory) AcquireBlocking(ctx context.Context, resource string, ttl time.Duration) (func(), error) {
	l := f.CreateLock(resource, ttl)
	if _, err := l.Acquire(ctx, true); err != nil {
		return nil, err
	}
	return func() { _ = l.Release(context.WithoutCancel(ctx)) }, nil
}
