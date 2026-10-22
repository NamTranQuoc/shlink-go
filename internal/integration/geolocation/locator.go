package geolocation

import (
	"context"
	"errors"

	"github.com/NamTranQuoc/shlink-go/internal/core"
)

// UnlocatableIPType ports UnlocatableIpType.
type UnlocatableIPType int

const (
	EmptyAddress UnlocatableIPType = iota
	Localhost
	LocateError
)

// IPCannotBeLocatedError ports IpCannotBeLocatedException.
type IPCannotBeLocatedError struct {
	Type UnlocatableIPType
	Prev error
}

func (e *IPCannotBeLocatedError) Error() string {
	switch e.Type {
	case EmptyAddress:
		return "Ignored visit with no IP address"
	case Localhost:
		return "Ignored localhost address"
	}
	return "An error occurred while locating IP"
}

func (e *IPCannotBeLocatedError) Unwrap() error { return e.Prev }

// IsNonLocatableAddress ports IpCannotBeLocatedException::isNonLocatableAddress().
func (e *IPCannotBeLocatedError) IsNonLocatableAddress() bool { return e.Type != LocateError }

// VisitToLocation ports VisitToLocationHelper.
type VisitToLocation struct {
	Resolver *Resolver
}

// ResolveVisitLocation ports VisitToLocationHelper::resolveVisitLocation().
func (h *VisitToLocation) ResolveVisitLocation(v *core.Visit) (core.Location, error) {
	if v.RemoteAddr == nil || *v.RemoteAddr == "" || *v.RemoteAddr == "0" {
		return core.Location{}, &IPCannotBeLocatedError{Type: EmptyAddress}
	}
	if *v.RemoteAddr == "127.0.0.1" {
		return core.Location{}, &IPCannotBeLocatedError{Type: Localhost}
	}
	loc, err := h.Resolver.ResolveIPLocation(*v.RemoteAddr)
	var wrong *WrongIPError
	if errors.As(err, &wrong) {
		return core.Location{}, &IPCannotBeLocatedError{Type: LocateError, Prev: err}
	}
	if err != nil {
		return core.Location{}, err
	}
	return *loc, nil
}

// VisitGeolocationHelper ports VisitGeolocationHelperInterface.
type VisitGeolocationHelper interface {
	GeolocateVisit(v *core.Visit) (core.Location, error)
	OnVisitLocated(location core.VisitLocation, v *core.Visit)
}

// VisitLocator ports VisitLocator.
type VisitLocator struct {
	Store *core.Store
}

// LocateUnlocatedVisits ports VisitLocator::locateUnlocatedVisits().
func (l *VisitLocator) LocateUnlocatedVisits(ctx context.Context, h VisitGeolocationHelper) error {
	return l.locate(ctx, core.UnlocatedVisits, h)
}

// LocateVisitsWithEmptyLocation ports VisitLocator::locateVisitsWithEmptyLocation().
func (l *VisitLocator) LocateVisitsWithEmptyLocation(ctx context.Context, h VisitGeolocationHelper) error {
	return l.locate(ctx, core.VisitsWithEmptyLocation, h)
}

// LocateAllVisits ports VisitLocator::locateAllVisits().
func (l *VisitLocator) LocateAllVisits(ctx context.Context, h VisitGeolocationHelper) error {
	return l.locate(ctx, core.AllVisits, h)
}

// locate ports VisitLocator::locateVisits(). An error locating a visit skips it, unless its address can never be
// located, in which case it gets an empty location so it is not picked again. Other errors abort, like an uncaught
// exception upstream.
func (l *VisitLocator) locate(ctx context.Context, filter core.VisitIterationFilter, h VisitGeolocationHelper) error {
	return l.Store.IterateVisits(ctx, filter, core.DateRange{}, false, func(_ int, v *core.Visit) error {
		location, err := h.GeolocateVisit(v)
		var cannot *IPCannotBeLocatedError
		if errors.As(err, &cannot) {
			if !cannot.IsNonLocatableAddress() {
				return nil
			}
			location, err = core.Location{}, nil
		}
		if err != nil {
			return err
		}
		vl := core.NewVisitLocation(location.CountryCode, location.CountryName, location.RegionName, location.City,
			location.Latitude, location.Longitude, location.TimeZone)
		if err := l.Store.LocateVisit(ctx, v, vl); err != nil {
			return err
		}
		h.OnVisitLocated(*v.Location, v)
		return nil
	})
}

// LocateUnlocatedVisits ports EventDispatcher\LocateUnlocatedVisits, run when a GeoLite2 db is first created.
type LocateUnlocatedVisits struct {
	Locator         *VisitLocator
	VisitToLocation *VisitToLocation
}

// Handle ports LocateUnlocatedVisits::__invoke(). Errors abort the run, like an exception escaping the listener.
func (l *LocateUnlocatedVisits) Handle(ctx context.Context) error {
	return l.Locator.LocateUnlocatedVisits(ctx, l)
}

// GeolocateVisit implements VisitGeolocationHelper.
func (l *LocateUnlocatedVisits) GeolocateVisit(v *core.Visit) (core.Location, error) {
	return l.VisitToLocation.ResolveVisitLocation(v)
}

// OnVisitLocated implements VisitGeolocationHelper.
func (l *LocateUnlocatedVisits) OnVisitLocated(core.VisitLocation, *core.Visit) {}
