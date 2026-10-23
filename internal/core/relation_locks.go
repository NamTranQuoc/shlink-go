package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/db"
)

// Locker ports the LockFactory PersistenceShortUrlRelationResolver gets.
type Locker interface {
	// AcquireBlocking waits until the named lock is held and returns its release function.
	AcquireBlocking(ctx context.Context, resource string, ttl time.Duration) (func(), error)
}

// relationLockTTL is the TTL PersistenceShortUrlRelationResolver::lock() creates its locks with.
const relationLockTTL = 5 * time.Second

// relationLocks holds the locks PersistenceShortUrlRelationResolver takes while it resolves domains and tags: a lock
// on an existing one is released right away, a lock on a new one is kept until the entity manager flushes.
type relationLocks struct {
	locker Locker
	held   map[string]func()
	// checked are the names already resolved in this transaction; they are not locked again.
	checked map[string]bool
}

type relationLocksKey struct{}

// withRelationLocks returns a context whose domain and tag resolution takes relation locks, and the function that
// releases the locks still held, like the resolver's postFlush().
func (s *Store) withRelationLocks(ctx context.Context) (context.Context, func()) {
	if s.Locks == nil {
		return ctx, func() {}
	}
	l := &relationLocks{locker: s.Locks, held: map[string]func(){}, checked: map[string]bool{}}
	return context.WithValue(ctx, relationLocksKey{}, l), l.releaseAll
}

func relationLocksFrom(ctx context.Context) *relationLocks {
	l, _ := ctx.Value(relationLocksKey{}).(*relationLocks)
	return l
}

func (l *relationLocks) lock(ctx context.Context, name string) error {
	if l == nil || l.checked[name] {
		return nil
	}
	if _, ok := l.held[name]; ok {
		return nil
	}
	release, err := l.locker.AcquireBlocking(ctx, name, relationLockTTL)
	if err != nil {
		return err
	}
	l.held[name] = release
	return nil
}

func (l *relationLocks) release(name string) {
	if l == nil {
		return
	}
	if release, ok := l.held[name]; ok {
		release()
		delete(l.held, name)
	}
}

func (l *relationLocks) releaseAll() {
	for name, release := range l.held {
		release()
		delete(l.held, name)
	}
	clear(l.checked)
}

// lockRelations ports the locking done by resolveTags() and resolveDomain() while the entity is built, before the
// transaction writes anything: each tag, then the domain, is locked and looked up, and the lock is released when it
// exists. Taking the locks before the first write keeps a transaction from waiting on a lock while it holds write
// locks in the database.
func (s *Store) lockRelations(ctx context.Context, q db.Querier, tags []string, domain *string) error {
	l := relationLocksFrom(ctx)
	if l == nil {
		return nil
	}
	one := 1
	exists := func(query, value string) (bool, error) {
		var id int64
		err := s.DB.QueryRow(ctx, q, s.DB.Limit(query, &one, nil), value).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	resolve := func(name, query, value string) error {
		if l.checked[name] {
			return nil
		}
		if err := l.lock(ctx, name); err != nil {
			return err
		}
		l.checked[name] = true
		found, err := exists(query, value)
		if found {
			l.release(name)
		}
		return err
	}
	for _, tag := range tags {
		if err := resolve("tag_"+tag, "SELECT id FROM tags WHERE name = ? ORDER BY id", tag); err != nil {
			return err
		}
	}
	if domain != nil {
		return resolve("domain_"+*domain, "SELECT id FROM domains WHERE authority = ? ORDER BY id", *domain)
	}
	return nil
}
