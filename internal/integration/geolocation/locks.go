package geolocation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

// Locks ports the blocking locks Shlink creates through Symfony's LockFactory with a FlockStore over data/locks: an
// in-process mutex plus an flock on the same file name Symfony uses, so CLI processes and the server exclude each
// other.
type Locks struct {
	Dir string

	mu    sync.Mutex
	local map[string]*sync.Mutex
}

var lockKeyChars = regexp.MustCompile(`(?i)[^a-z0-9._-]+`)

// lockFile ports FlockStore's lock file naming.
func (l *Locks) lockFile(key string) string {
	sum := sha256.Sum256([]byte(key))
	suffix := strings.NewReplacer("/", "_").Replace(base64.StdEncoding.EncodeToString(sum[:])[:7])
	return filepath.Join(l.Dir, "sf."+lockKeyChars.ReplaceAllString(key, "-")+"."+suffix+".lock")
}

// Acquire blocks until the named lock is held, and returns its release function.
func (l *Locks) Acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	if l.local == nil {
		l.local = map[string]*sync.Mutex{}
	}
	m, ok := l.local[key]
	if !ok {
		m = &sync.Mutex{}
		l.local[key] = m
	}
	l.mu.Unlock()
	m.Lock()
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		m.Unlock()
		return nil, err
	}
	f, err := os.OpenFile(l.lockFile(key), os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		m.Unlock()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		m.Unlock()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		m.Unlock()
	}, nil
}
