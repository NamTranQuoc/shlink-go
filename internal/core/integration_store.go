package core

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// visitColumns selects a visit plus its location (aliases v and vl), in the order scanVisit expects.
func (s *Store) visitColumns() string {
	return `v.id, v.short_url_id, v.type, v.user_agent, v.referer, v.potential_bot, v.remote_addr, v.visited_url,
		v.redirect_url, v.` + s.DB.Q("date") + `, vl.id, vl.country_code, vl.country_name, vl.region_name, vl.city_name,
		vl.lat, vl.lon, vl.timezone, vl.is_empty`
}

// FindShortURLByID finds a short URL by its id, like EntityManager::find(ShortUrl::class, $id). It returns nil when
// it does not exist.
func (s *Store) FindShortURLByID(ctx context.Context, id int64) (*ShortURL, error) {
	one := 1
	query := s.DB.Limit("SELECT "+shortURLColumns+shortURLFrom+" WHERE s.id = ? ORDER BY s.id", &one, nil)
	su, err := s.scanShortURL(s.DB.QueryRow(ctx, s.DB.DB, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return su, s.loadTags(ctx, s.DB.DB, su)
}

// FindVisitByID finds a visit with its location and short URL, like EntityManager::find(Visit::class, $id). It
// returns nil when it does not exist.
func (s *Store) FindVisitByID(ctx context.Context, id int64) (*Visit, error) {
	query := "SELECT " + s.visitColumns() + " FROM visits v LEFT JOIN visit_locations vl ON vl.id = v.visit_location_id " +
		"WHERE v.id = ?"
	v, err := s.scanVisit(s.DB.QueryRow(ctx, s.DB.DB, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.ShortURLID != nil {
		if v.ShortURL, err = s.FindShortURLByID(ctx, *v.ShortURLID); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// VisitIterationFilter selects the visits VisitIterationRepository iterates over.
type VisitIterationFilter int

const (
	// UnlocatedVisits ports findUnlocatedVisits(): visits without location.
	UnlocatedVisits VisitIterationFilter = iota
	// VisitsWithEmptyLocation ports findVisitsWithEmptyLocation().
	VisitsWithEmptyLocation
	// AllVisits ports findAllVisits(), optionally within a date range.
	AllVisits
)

// DefaultVisitsBlockSize ports VisitIterationRepositoryInterface::DEFAULT_BLOCK_SIZE.
const DefaultVisitsBlockSize = 10000

// IterateVisits ports VisitIterationRepository: visits are read in blocks ordered by id, each block starting after the
// last id of the previous one, and passed to fn one by one with their position. Short URLs are loaded when
// withShortURL is true.
func (s *Store) IterateVisits(ctx context.Context, filter VisitIterationFilter, dateRange DateRange, withShortURL bool,
	fn func(index int, v *Visit) error) error {
	var lastID int64
	index := 0
	block := DefaultVisitsBlockSize
	for {
		conds := []string{"v.id > ?"}
		args := []any{lastID}
		from := " FROM visits v LEFT JOIN visit_locations vl ON vl.id = v.visit_location_id"
		switch filter {
		case UnlocatedVisits:
			conds = append(conds, "v.visit_location_id IS NULL")
		case VisitsWithEmptyLocation:
			conds = append(conds, "v.visit_location_id IS NOT NULL", "vl.is_empty = ?")
			args = append(args, true)
		case AllVisits:
			if dateRange.Start != nil {
				conds = append(conds, "v."+s.DB.Q("date")+" >= ?")
				args = append(args, s.DB.FormatTime(*dateRange.Start))
			}
			if dateRange.End != nil {
				conds = append(conds, "v."+s.DB.Q("date")+" <= ?")
				args = append(args, s.DB.FormatTime(*dateRange.End))
			}
		}
		query := s.DB.Limit("SELECT "+s.visitColumns()+from+where(conds)+" ORDER BY v.id ASC", &block, nil)
		visits, err := s.queryVisits(ctx, query, args...)
		if err != nil {
			return err
		}
		if len(visits) == 0 {
			return nil
		}
		for _, v := range visits {
			if withShortURL && v.ShortURLID != nil {
				if v.ShortURL, err = s.FindShortURLByID(ctx, *v.ShortURLID); err != nil {
					return err
				}
			}
			if err := fn(index, v); err != nil {
				return err
			}
			index++
		}
		lastID = visits[len(visits)-1].ID
	}
}

func (s *Store) queryVisits(ctx context.Context, query string, args ...any) ([]*Visit, error) {
	rows, err := s.DB.Query(ctx, s.DB.DB, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Visit
	for rows.Next() {
		v, err := s.scanVisit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LocateVisit ports VisitLocator::locateVisit(): the visit gets a new location and the previous one is removed so no
// orphan locations are left.
func (s *Store) LocateVisit(ctx context.Context, v *Visit, loc VisitLocation) error {
	return s.DB.InTx(ctx, func(tx *sql.Tx) error {
		id, err := s.insertLocation(ctx, tx, &loc)
		if err != nil {
			return err
		}
		if _, err := s.DB.Exec(ctx, tx, "UPDATE visits SET visit_location_id = ? WHERE id = ?", id, v.ID); err != nil {
			return err
		}
		if v.Location != nil && v.Location.ID != 0 {
			if _, err := s.DB.Exec(ctx, tx, "DELETE FROM visit_locations WHERE id = ?", v.Location.ID); err != nil {
				return err
			}
		}
		v.Location = &loc
		return nil
	})
}

// GeolocationDbUpdate ports the GeolocationDbUpdate entity.
type GeolocationDbUpdate struct {
	ID           int64
	Reason       string
	FilesystemID string
	Status       string
	DateCreated  time.Time
	DateUpdated  time.Time
	Error        *string
}

// GeolocationDbUpdate statuses, ported from GeolocationDbUpdateStatus.
const (
	GeolocationUpdateInProgress = "in-progress"
	GeolocationUpdateSuccess    = "success"
	GeolocationUpdateError      = "error"
)

// IsOlderThan ports GeolocationDbUpdate::isOlderThan().
func (u *GeolocationDbUpdate) IsOlderThan(days int, now time.Time) bool {
	return now.After(u.DateUpdated.AddDate(0, 0, days))
}

// RecentGeolocationDbUpdates ports findBy(['filesystemId' => ...], ['dateUpdated' => 'DESC'], limit).
func (s *Store) RecentGeolocationDbUpdates(ctx context.Context, filesystemID string, limit int) ([]*GeolocationDbUpdate, error) {
	query := s.DB.Limit("SELECT id, reason, filesystem_id, status, date_created, date_updated, error "+
		"FROM geolocation_db_updates WHERE filesystem_id = ? ORDER BY date_updated DESC", &limit, nil)
	rows, err := s.DB.Query(ctx, s.DB.DB, query, filesystemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GeolocationDbUpdate
	for rows.Next() {
		u := &GeolocationDbUpdate{}
		created, updated := s.nullTime(), s.nullTime()
		var errMsg sql.NullString
		if err := rows.Scan(&u.ID, &u.Reason, &u.FilesystemID, &u.Status, &created, &updated, &errMsg); err != nil {
			return nil, err
		}
		u.DateCreated, u.DateUpdated, u.Error = created.Time, updated.Time, strPtr(errMsg)
		out = append(out, u)
	}
	return out, rows.Err()
}

// SaveGeolocationDbUpdate inserts or updates a GeolocationDbUpdate, like persist() + flush().
func (s *Store) SaveGeolocationDbUpdate(ctx context.Context, u *GeolocationDbUpdate) error {
	if u.ID == 0 {
		id, err := s.DB.Insert(ctx, s.DB.DB, "geolocation_db_updates",
			[]string{"date_created", "date_updated", "status", "error", "reason", "filesystem_id"},
			s.DB.FormatTime(u.DateCreated), s.DB.FormatTime(u.DateUpdated), u.Status, nullable(u.Error), u.Reason,
			u.FilesystemID)
		u.ID = id
		return err
	}
	_, err := s.DB.Exec(ctx, s.DB.DB, "UPDATE geolocation_db_updates SET date_updated = ?, status = ?, error = ? WHERE id = ?",
		s.DB.FormatTime(u.DateUpdated), u.Status, nullable(u.Error), u.ID)
	return err
}
