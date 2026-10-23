package lock

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// flockStore ports FlockStore: an flock on a file per resource, which CLI processes and the server share. It ignores
// TTLs. Goroutines of this process queue on processLocks first.
type flockStore struct {
	dir     string
	process processLocks
}

var lockKeyChars = regexp.MustCompile(`(?i)[^a-z0-9._-]+`)

// lockFile ports FlockStore's lock file naming.
func (s *flockStore) lockFile(resource string) string {
	sum := sha256.Sum256([]byte(resource))
	suffix := strings.NewReplacer("/", "_").Replace(base64.StdEncoding.EncodeToString(sum[:])[:7])
	return filepath.Join(s.dir, "sf."+lockKeyChars.ReplaceAllString(resource, "-")+"."+suffix+".lock")
}

func (s *flockStore) save(ctx context.Context, k *key) error {
	return s.lock(ctx, k, false)
}

func (s *flockStore) waitAndSave(ctx context.Context, k *key) (bool, error) {
	return true, s.lock(ctx, k, true)
}

func (s *flockStore) lock(ctx context.Context, k *key, blocking bool) error {
	if k.release != nil {
		return nil
	}
	releaseProcess, err := s.process.lock(ctx, k.resource, blocking)
	if err != nil {
		return err
	}
	if releaseProcess == nil {
		return errConflicted
	}
	f, err := s.open(k.resource)
	if err != nil {
		releaseProcess()
		return err
	}
	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		releaseProcess()
		if err == syscall.EWOULDBLOCK {
			return errConflicted
		}
		return err
	}
	k.release = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		releaseProcess()
	}
	return nil
}

func (s *flockStore) open(resource string) (*os.File, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(s.lockFile(resource), os.O_RDWR|os.O_CREATE, 0o666)
}

func (s *flockStore) putOffExpiration(context.Context, *key, time.Duration) error { return nil }

func (s *flockStore) delete(_ context.Context, k *key) error {
	if k.release != nil {
		k.release()
		k.release = nil
	}
	return nil
}
