package geolocation

import (
	"os"
	"syscall"
)

// TryAcquire takes the named lock without waiting, like a non-blocking Symfony lock. It returns a nil release when
// the lock is already held.
func (l *Locks) TryAcquire(key string) (func(), error) {
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(l.lockFile(key), os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, nil
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
