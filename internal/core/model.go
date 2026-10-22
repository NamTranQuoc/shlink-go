package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// DefaultAuthority is the special domain name used to filter by the default domain.
const DefaultAuthority = "DEFAULT"

// Domain ports the Domain entity.
type Domain struct {
	ID                      int64
	Authority               string
	BaseURLRedirect         *string
	Regular404Redirect      *string
	InvalidShortURLRedirect *string
}

// NotFoundRedirects ports Config\NotFoundRedirects.
type NotFoundRedirects struct {
	BaseURLRedirect         *string `json:"baseUrlRedirect"`
	Regular404Redirect      *string `json:"regular404Redirect"`
	InvalidShortURLRedirect *string `json:"invalidShortUrlRedirect"`
}

// Redirects returns the domain's not-found redirects.
func (d *Domain) Redirects() NotFoundRedirects {
	return NotFoundRedirects{d.BaseURLRedirect, d.Regular404Redirect, d.InvalidShortURLRedirect}
}

// ShortURL ports the ShortUrl entity. Tags are kept sorted by name, like the ORM mapping orders them.
type ShortURL struct {
	ID                      int64
	LongURL                 string
	ShortCode               string
	DateCreated             time.Time
	ValidSince              *time.Time
	ValidUntil              *time.Time
	MaxVisits               *int
	ImportSource            *string
	ImportOriginalShortCode *string
	Title                   *string
	TitleWasAutoResolved    bool
	Crawlable               bool
	ForwardQuery            bool
	Domain                  *Domain
	AuthorAPIKeyID          *int64
	Tags                    []string
	// TagKeys holds the PHP array keys of Tags when they were just set from a payload: TagsConverter keeps the
	// payload's keys and array_unique() the keys of first occurrences. Nil means sequential keys.
	TagKeys []string

	// Set only for entities that have not been persisted yet.
	customSlugWasProvided bool
	shortCodeLength       int
}

// LongURLHash ports stringToBinHash().
func LongURLHash(longURL string) []byte {
	sum := sha256.Sum256([]byte(longURL))
	return sum[:]
}

// Authority returns the domain authority or nil.
func (s *ShortURL) Authority() *string {
	if s.Domain == nil {
		return nil
	}
	a := s.Domain.Authority
	return &a
}

// Identifier ports ShortUrlIdentifier::fromShortUrl().
func (s *ShortURL) Identifier() ShortURLIdentifier {
	return ShortURLIdentifier{ShortCode: s.ShortCode, Domain: s.Authority()}
}

// IsEnabled ports ShortUrl::isEnabled(); the visits count is needed to evaluate max visits.
func (s *ShortURL) IsEnabled(visitsCount int, now time.Time) bool {
	if s.MaxVisits != nil && visitsCount >= *s.MaxVisits {
		return false
	}
	if s.ValidSince != nil && s.ValidSince.After(now) {
		return false
	}
	if s.ValidUntil != nil && s.ValidUntil.Before(now) {
		return false
	}
	return true
}

// ShortURLIdentifier ports ShortUrlIdentifier.
type ShortURLIdentifier struct {
	ShortCode string
	Domain    *string
}

// NewIdentifier builds an identifier, treating an empty domain pointer as nil.
func NewIdentifier(shortCode string, domain *string) ShortURLIdentifier {
	return ShortURLIdentifier{ShortCode: shortCode, Domain: domain}
}

func (i ShortURLIdentifier) String() string {
	if i.Domain == nil {
		return i.ShortCode
	}
	return fmt.Sprintf("%s/%s", *i.Domain, i.ShortCode)
}

// VisitsSummary ports Visit\Model\VisitsSummary.
type VisitsSummary struct {
	Total   int
	NonBots int
}

// EncodePHP serializes like VisitsSummary::jsonSerialize().
func (v VisitsSummary) EncodePHP(int) any {
	return phpx.Map{{Key: "total", Value: v.Total}, {Key: "nonBots", Value: v.NonBots}, {Key: "bots", Value: v.Total - v.NonBots}}
}

// VisitType ports Visit\Model\VisitType.
type VisitType string

const (
	VisitValidShortURL   VisitType = "valid_short_url"
	VisitImported        VisitType = "imported"
	VisitInvalidShortURL VisitType = "invalid_short_url"
	VisitBaseURL         VisitType = "base_url"
	VisitRegular404      VisitType = "regular_404"
)

// OrphanVisitTypes lists the orphan visit types, in upstream's order.
var OrphanVisitTypes = []VisitType{VisitInvalidShortURL, VisitBaseURL, VisitRegular404}

// VisitLocation ports the VisitLocation entity.
type VisitLocation struct {
	ID          int64
	CountryCode string
	CountryName string
	RegionName  string
	CityName    string
	Latitude    float64
	Longitude   float64
	Timezone    string
	IsEmpty     bool
}

// NewVisitLocation computes isEmpty like the entity constructor does.
func NewVisitLocation(countryCode, countryName, regionName, cityName string, lat, lon float64, timezone string) VisitLocation {
	return VisitLocation{
		CountryCode: countryCode, CountryName: countryName, RegionName: regionName, CityName: cityName,
		Latitude: lat, Longitude: lon, Timezone: timezone,
		IsEmpty: countryCode == "" && countryName == "" && regionName == "" && cityName == "" && lat == 0 && lon == 0 && timezone == "",
	}
}

// Visit ports the Visit entity.
type Visit struct {
	ID           int64
	ShortURLID   *int64
	ShortURL     *ShortURL
	Type         VisitType
	UserAgent    string
	Referer      string
	PotentialBot bool
	RemoteAddr   *string
	VisitedURL   *string
	RedirectURL  *string
	Location     *VisitLocation
	Date         time.Time
}

// IsOrphan tells if the visit does not belong to a short URL.
func (v *Visit) IsOrphan() bool { return v.ShortURLID == nil }

// Tag ports the Tag entity.
type Tag struct {
	ID   int64
	Name string
}

// Renaming ports Model\Renaming.
type Renaming struct {
	OldName string
	NewName string
}

func (r Renaming) NameChanged() bool { return r.OldName != r.NewName }
func (r Renaming) String() string    { return fmt.Sprintf("%s to %s", r.OldName, r.NewName) }

// Role ports Rest\ApiKey\Role.
type Role string

const (
	RoleAuthoredShortURLs Role = "AUTHORED_SHORT_URLS"
	RoleDomainSpecific    Role = "DOMAIN_SPECIFIC"
	RoleNoOrphanVisits    Role = "NO_ORPHAN_VISITS"
)

// APIKeyRole ports the ApiKeyRole entity. Meta is the decoded JSON object (or empty).
type APIKeyRole struct {
	ID   int64
	Role Role
	Meta map[string]any
}

// DomainID ports Role::domainIdFromMeta().
func (r APIKeyRole) DomainID() string {
	if v, ok := r.Meta["domain_id"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return "-1"
}

// DomainAuthority ports Role::domainAuthorityFromMeta().
func (r APIKeyRole) DomainAuthority() string {
	if v, ok := r.Meta["authority"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// FriendlyName ports Role::toFriendlyName().
func (r APIKeyRole) FriendlyName() string {
	switch r.Role {
	case RoleAuthoredShortURLs:
		return "Author only"
	case RoleDomainSpecific:
		return "Domain only: " + r.DomainAuthority()
	case RoleNoOrphanVisits:
		return "No orphan visits"
	}
	return string(r.Role)
}

// APIKey ports the ApiKey entity. Key holds the sha256 hash of the plain key. Roles are kept in insertion order.
type APIKey struct {
	ID             int64
	Key            string
	Name           string
	ExpirationDate *time.Time
	Enabled        bool
	Roles          []APIKeyRole
}

// HashAPIKey ports ApiKey::hashKey().
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// IsExpired ports ApiKey::isExpired().
func (k *APIKey) IsExpired(now time.Time) bool {
	return k.ExpirationDate != nil && k.ExpirationDate.Before(now)
}

// IsValid ports ApiKey::isValid().
func (k *APIKey) IsValid(now time.Time) bool { return k.Enabled && !k.IsExpired(now) }

// HasRole ports ApiKey::hasRole().
func (k *APIKey) HasRole(role Role) bool {
	_, ok := k.Role(role)
	return ok
}

// Role returns the role with given name.
func (k *APIKey) Role(role Role) (APIKeyRole, bool) {
	if k == nil {
		return APIKeyRole{}, false
	}
	for _, r := range k.Roles {
		if r.Role == role {
			return r, true
		}
	}
	return APIKeyRole{}, false
}

// IsAdmin ports ApiKey::isAdmin(): no key (CLI) or a key without roles.
func IsAdmin(k *APIKey) bool { return k == nil || len(k.Roles) == 0 }

// IsShortURLRestricted ports ApiKey::isShortUrlRestricted().
func IsShortURLRestricted(k *APIKey) bool {
	return k != nil && (k.HasRole(RoleAuthoredShortURLs) || k.HasRole(RoleDomainSpecific))
}

// RoleDefinition ports Rest\ApiKey\Model\RoleDefinition.
type RoleDefinition struct {
	Role Role
	Meta map[string]any
}
