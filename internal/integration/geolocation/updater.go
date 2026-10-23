package geolocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/integration/lock"
)

// Result ports GeolocationResult.
type Result int

const (
	CheckSkipped Result = iota
	MaxErrorsReached
	LicenseMissing
	DBCreated
	DBUpdated
	DBIsUpToDate
	UpdateInProgress
)

// ProgressHandler ports GeolocationDownloadProgressHandlerInterface.
type ProgressHandler interface {
	BeforeDownload(olderDBExists bool)
	HandleProgress(total, downloaded int64, olderDBExists bool)
}

// DbUpdateFailedError ports GeolocationDbUpdateFailedException.
type DbUpdateFailedError struct {
	OlderDBExists bool
	Prev          error
}

func (e *DbUpdateFailedError) Error() string {
	if e.OlderDBExists {
		return "An error occurred while updating geolocation database, but an older DB is already present."
	}
	return "An error occurred while updating geolocation database, and an older version could not be found."
}

func (e *DbUpdateFailedError) Unwrap() error { return e.Prev }

// maxRecentAttemptsToCheck ports GeolocationDbUpdater's $maxRecentAttemptsToCheck default.
const maxRecentAttemptsToCheck = 15

// GeolocationDbUpdater ports Shlink's GeolocationDbUpdater.
type GeolocationDbUpdater struct {
	Updater  *DbUpdater
	Store    *core.Store
	Tracking config.TrackingOptions
	// Locks is the LOCAL_LOCK_FACTORY upstream injects: always a FlockStore.
	Locks *lock.Factory
	Now   func() time.Time
}

// currentFilesystemID ports GeolocationDbUpdate::currentFilesystemId(): the device id of the filesystem holding the
// running code (the executable here).
func currentFilesystemID() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", errors.New("It was not possible to resolve filesystem ID via stat function")
	}
	info, err := os.Stat(exe)
	if err != nil {
		return "", errors.New("It was not possible to resolve filesystem ID via stat function")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return strconv.FormatUint(uint64(st.Dev), 10), nil
	}
	return "0", nil
}

func (u *GeolocationDbUpdater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// CheckDbUpdate ports GeolocationDbUpdater::checkDbUpdate().
func (u *GeolocationDbUpdater) CheckDbUpdate(ctx context.Context, handler ProgressHandler) (Result, error) {
	if !u.Tracking.IsGeolocationRelevant() {
		return CheckSkipped, nil
	}
	l := u.Locks.CreateLock("geolocation-db-update", lock.DefaultTTL)
	if _, err := l.Acquire(ctx, true); err != nil {
		return 0, err
	}
	defer func() { _ = l.Release(ctx) }()
	return u.downloadIfNeeded(ctx, handler)
}

func (u *GeolocationDbUpdater) downloadIfNeeded(ctx context.Context, handler ProgressHandler) (Result, error) {
	fsID, err := currentFilesystemID()
	if err != nil {
		return 0, err
	}
	recent, err := u.Store.RecentGeolocationDbUpdates(ctx, fsID, maxRecentAttemptsToCheck)
	if err != nil {
		return 0, err
	}
	var mostRecent *core.GeolocationDbUpdate
	if len(recent) > 0 {
		mostRecent = recent[0]
	}
	if mostRecent != nil && mostRecent.Status == core.GeolocationUpdateInProgress {
		return UpdateInProgress, nil
	}
	errorsSinceLastSuccess := 0
	for _, r := range recent {
		if r.Status == core.GeolocationUpdateSuccess {
			break
		}
		errorsSinceLastSuccess++
	}
	now := u.now()
	lastAttemptIsError := mostRecent != nil && mostRecent.Status == core.GeolocationUpdateError
	maxErrorsReached := errorsSinceLastSuccess == maxRecentAttemptsToCheck
	if lastAttemptIsError && maxErrorsReached && !mostRecent.IsOlderThan(2, now) {
		return MaxErrorsReached, nil
	}
	var olderDBExists bool
	var reason string
	switch {
	case mostRecent == nil:
		olderDBExists, reason = false, "No download attempts tracked for this instance"
	case !u.Updater.DatabaseFileExists():
		olderDBExists, reason = false, "Geolocation db file does not exist"
	case lastAttemptIsError:
		olderDBExists, reason = true, "Max consecutive errors not reached"
	case mostRecent.IsOlderThan(30, now):
		olderDBExists, reason = true, "Last successful attempt is old enough"
	default:
		return DBIsUpToDate, nil
	}
	return u.downloadAndTrackUpdate(ctx, handler, olderDBExists, reason, fsID)
}

func (u *GeolocationDbUpdater) downloadAndTrackUpdate(ctx context.Context, handler ProgressHandler, olderDBExists bool,
	reason, fsID string) (Result, error) {
	now := u.now()
	update := &core.GeolocationDbUpdate{
		Reason: reason, FilesystemID: fsID, Status: core.GeolocationUpdateInProgress, DateCreated: now, DateUpdated: now,
	}
	if err := u.Store.SaveGeolocationDbUpdate(ctx, update); err != nil {
		return 0, err
	}
	finish := func(status string, errMsg *string) error {
		update.Status, update.Error, update.DateUpdated = status, errMsg, u.now()
		return u.Store.SaveGeolocationDbUpdate(context.WithoutCancel(ctx), update)
	}
	result, err := u.downloadNewDB(ctx, handler, olderDBExists)
	if err == nil {
		return result, finish(core.GeolocationUpdateSuccess, nil)
	}
	var failed *DbUpdateFailedError
	switch {
	case errors.Is(err, ErrMissingLicense):
		msg := "Geolocation license key is missing"
		return LicenseMissing, finish(core.GeolocationUpdateError, &msg)
	case errors.As(err, &failed):
		prev := "-"
		if failed.Prev != nil {
			prev = failed.Prev.Error()
		}
		msg := fmt.Sprintf("%s Prev: %s", failed.Error(), prev)
		_ = finish(core.GeolocationUpdateError, &msg)
		return 0, err
	default:
		msg := fmt.Sprintf("Unknown error: %s", err.Error())
		_ = finish(core.GeolocationUpdateError, &msg)
		return 0, err
	}
}

func (u *GeolocationDbUpdater) downloadNewDB(ctx context.Context, handler ProgressHandler, olderDBExists bool) (Result, error) {
	if handler != nil {
		handler.BeforeDownload(olderDBExists)
	}
	err := u.Updater.DownloadFreshCopy(ctx, func(total, downloaded int64) {
		if handler != nil {
			handler.HandleProgress(total, downloaded, olderDBExists)
		}
	})
	var dbErr *DbUpdateError
	if errors.As(err, &dbErr) {
		return 0, &DbUpdateFailedError{OlderDBExists: olderDBExists, Prev: err}
	}
	if err != nil {
		return 0, err
	}
	if olderDBExists {
		return DBUpdated, nil
	}
	return DBCreated, nil
}

// GeoLiteDbCreatedDispatcher dispatches GeoLiteDbCreated.
type GeoLiteDbCreatedDispatcher interface {
	GeoLiteDbCreated(ctx context.Context)
}

// UpdateGeoLiteDb ports EventDispatcher\UpdateGeoLiteDb, run after every tracked visit.
type UpdateGeoLiteDb struct {
	Updater *GeolocationDbUpdater
	Events  GeoLiteDbCreatedDispatcher
	Logger  *slog.Logger
}

type loggingProgress struct {
	logger *slog.Logger
	logged bool
}

func (p *loggingProgress) BeforeDownload(olderDBExists bool) {
	action := "Downloading"
	if olderDBExists {
		action = "Updating"
	}
	p.logger.Log(context.Background(), LevelNotice, action+" GeoLite2 db file...")
}

func (p *loggingProgress) HandleProgress(total, downloaded int64, olderDBExists bool) {
	if p.logged || total > downloaded {
		return
	}
	p.logged = true
	action := "downloading"
	if olderDBExists {
		action = "updating"
	}
	p.logger.Log(context.Background(), LevelNotice, "Finished "+action+" GeoLite2 db file")
}

// LevelNotice is PSR-3's notice level, between info and warning.
const LevelNotice = slog.LevelInfo + 2

// Handle ports UpdateGeoLiteDb::__invoke().
func (l *UpdateGeoLiteDb) Handle(ctx context.Context, _ int64, _ *string) {
	result, err := l.Updater.CheckDbUpdate(ctx, &loggingProgress{logger: l.Logger})
	if err != nil {
		l.Logger.Error(fmt.Sprintf("GeoLite2 database download failed. %v", err), "e", err)
		return
	}
	if result == DBCreated && l.Events != nil {
		l.Events.GeoLiteDbCreated(ctx)
	}
}
