package config

import (
	"fmt"
	"strings"
)

// Version is the upstream version this port reproduces; it is reported by /rest/health and the CLI.
const Version = "5.1.8"

// ShortURLMode ports ShortUrlMode.
type ShortURLMode string

const (
	ModeStrict ShortURLMode = "strict"
	ModeLoose  ShortURLMode = "loose"
)

// ExtraPathMode ports Config\Options\ExtraPathMode.
type ExtraPathMode string

const (
	ExtraPathDefault ExtraPathMode = "default"
	ExtraPathAppend  ExtraPathMode = "append"
	ExtraPathIgnore  ExtraPathMode = "ignore"
)

// URLShortenerOptions ports UrlShortenerOptions.
type URLShortenerOptions struct {
	DefaultDomain            string
	Schema                   string
	DefaultShortCodesLength  int
	AutoResolveTitles        bool
	MultiSegmentSlugsEnabled bool
	TrailingSlashEnabled     bool
	Mode                     ShortURLMode
	ExtraPathMode            ExtraPathMode
}

func (o URLShortenerOptions) IsLooseMode() bool { return o.Mode == ModeLoose }

func URLShortenerOptionsFromEnv() URLShortenerOptions {
	length := DefaultShortCodesLength.Load().AsInt()
	if length < MinShortCodesLength {
		length = MinShortCodesLength
	}
	extra := ExtraPathMode(RedirectExtraPathMode.Load().AsString())
	if extra != ExtraPathAppend && extra != ExtraPathIgnore {
		extra = ExtraPathDefault
	}
	mode := ShortURLMode(ShortURLModeVar.Load().AsString())
	if mode != ModeLoose {
		mode = ModeStrict
	}
	schema := "http"
	if IsHTTPSEnabled.Load().AsBool() {
		schema = "https"
	}
	return URLShortenerOptions{
		DefaultDomain:            DefaultDomain.Load().AsString(),
		Schema:                   schema,
		DefaultShortCodesLength:  length,
		AutoResolveTitles:        AutoResolveTitles.Load().AsBool(),
		MultiSegmentSlugsEnabled: MultiSegmentSlugsEnabled.Load().AsBool(),
		TrailingSlashEnabled:     ShortURLTrailingSlash.Load().AsBool(),
		Mode:                     mode,
		ExtraPathMode:            extra,
	}
}

// TrackingOptions ports TrackingOptions.
type TrackingOptions struct {
	AnonymizeRemoteAddr     bool
	TrackOrphanVisits       bool
	DisableTrackParam       *string
	DisableTracking         bool
	DisableIPTracking       bool
	DisableReferrerTracking bool
	DisableUATracking       bool
	DisableTrackingFrom     []string
}

func TrackingOptionsFromEnv() TrackingOptions {
	return TrackingOptions{
		AnonymizeRemoteAddr:     AnonymizeRemoteAddr.Load().AsBool(),
		TrackOrphanVisits:       TrackOrphanVisits.Load().AsBool(),
		DisableTrackParam:       DisableTrackParam.Load().StringOrNil(),
		DisableTracking:         DisableTracking.Load().AsBool(),
		DisableIPTracking:       DisableIPTracking.Load().AsBool(),
		DisableReferrerTracking: DisableReferrerTracking.Load().AsBool(),
		DisableUATracking:       DisableUATracking.Load().AsBool(),
		DisableTrackingFrom:     SplitByComma(DisableTrackingFrom.Load()),
	}
}

// IsGeolocationRelevant ports TrackingOptions::isGeolocationRelevant().
func (o TrackingOptions) IsGeolocationRelevant() bool {
	return !o.DisableTracking && !o.DisableIPTracking
}

// RedirectOptions ports RedirectOptions.
type RedirectOptions struct {
	StatusCode      int
	CacheLifetime   int
	CacheVisibility string
}

var validRedirectStatuses = map[int]bool{301: true, 302: true, 307: true, 308: true}

func NewRedirectOptions(status, lifetime int, visibility *string) RedirectOptions {
	o := RedirectOptions{StatusCode: 302, CacheLifetime: DefaultRedirectCacheLifetime, CacheVisibility: DefaultRedirectCacheVisibility}
	if validRedirectStatuses[status] {
		o.StatusCode = status
	}
	if lifetime > 0 {
		o.CacheLifetime = lifetime
	}
	if visibility != nil && (*visibility == "public" || *visibility == "private") {
		o.CacheVisibility = *visibility
	}
	return o
}

func RedirectOptionsFromEnv() RedirectOptions {
	return NewRedirectOptions(
		RedirectStatusCode.Load().AsInt(),
		RedirectCacheLifetime.Load().AsInt(),
		RedirectCacheVisibility.Load().StringOrNil(),
	)
}

// AllowedRedirectMethods ports RedirectStatus::allowedHttpMethods(): 307/308 keep the method, so every method is
// accepted; 301/302 only GET.
func (o RedirectOptions) AllowedRedirectMethods() []string {
	if o.StatusCode == 307 || o.StatusCode == 308 {
		return nil
	}
	return []string{"GET"}
}

// NotFoundRedirectOptions ports NotFoundRedirectOptions.
type NotFoundRedirectOptions struct {
	InvalidShortURLRedirect *string
	Regular404Redirect      *string
	BaseURLRedirect         *string
}

func NotFoundRedirectOptionsFromEnv() NotFoundRedirectOptions {
	return NotFoundRedirectOptions{
		InvalidShortURLRedirect: DefaultInvalidShortURLRedirect.Load().StringOrNil(),
		Regular404Redirect:      DefaultRegular404Redirect.Load().StringOrNil(),
		BaseURLRedirect:         DefaultBaseURLRedirect.Load().StringOrNil(),
	}
}

// RobotsOptions ports RobotsOptions.
type RobotsOptions struct {
	AllowAllShortURLs bool
	UserAgents        []string
}

func RobotsOptionsFromEnv() RobotsOptions {
	return RobotsOptions{
		AllowAllShortURLs: RobotsAllowAllShortURLs.Load().AsBool(),
		UserAgents:        SplitByComma(RobotsUserAgents.Load()),
	}
}

// DeleteShortURLsOptions ports DeleteShortUrlsOptions.
type DeleteShortURLsOptions struct {
	VisitsThreshold      int
	CheckVisitsThreshold bool
}

func DeleteShortURLsOptionsFromEnv() DeleteShortURLsOptions {
	v := DeleteShortURLThreshold.Load()
	threshold := DefaultDeleteShortURLThreshold
	if !v.IsNull() {
		threshold = v.AsInt()
	}
	return DeleteShortURLsOptions{VisitsThreshold: threshold, CheckVisitsThreshold: !v.IsNull()}
}

// Topic names, ported from EventDispatcher\Topic.
const (
	TopicNewVisit         = "NEW_VISIT"
	TopicNewShortURLVisit = "NEW_SHORT_URL_VISIT"
	TopicNewOrphanVisit   = "NEW_ORPHAN_VISIT"
	TopicNewShortURL      = "NEW_SHORT_URL"
)

// AllTopicNames lists the topic names in upstream's order.
var AllTopicNames = []string{TopicNewVisit, TopicNewShortURLVisit, TopicNewOrphanVisit, TopicNewShortURL}

// RealTimeUpdatesOptions ports RealTimeUpdatesOptions.
type RealTimeUpdatesOptions struct {
	EnabledTopics []string
}

// RealTimeUpdatesOptionsFromEnv returns an error for unknown topics, like upstream's ValidationException.
func RealTimeUpdatesOptionsFromEnv() (RealTimeUpdatesOptions, error) {
	topics := SplitByComma(RealTimeUpdatesTopics.Load())
	if len(topics) == 0 {
		return RealTimeUpdatesOptions{EnabledTopics: AllTopicNames}, nil
	}
	for _, t := range topics {
		valid := false
		for _, v := range AllTopicNames {
			if t == v {
				valid = true
			}
		}
		if !valid {
			return RealTimeUpdatesOptions{}, fmt.Errorf(`Real-time updates topic "%s" is not valid. Expected one of ["%s"].`,
				t, strings.Join(AllTopicNames, `", "`))
		}
	}
	return RealTimeUpdatesOptions{EnabledTopics: topics}, nil
}

// IsTopicEnabled ports RealTimeUpdatesOptions::isTopicEnabled().
func (o RealTimeUpdatesOptions) IsTopicEnabled(topic string) bool {
	for _, t := range o.EnabledTopics {
		if t == topic {
			return true
		}
	}
	return false
}

// CorsOptions ports CorsOptions.
type CorsOptions struct {
	// AllowOrigins is "*", "<origin>" or nil when Origins is used.
	AllowOrigins     string
	Origins          []string
	AllowCredentials bool
	MaxAge           int
}

func NewCorsOptions(allowOrigins string, allowCredentials bool, maxAge int) CorsOptions {
	lower := strings.ToLower(allowOrigins)
	o := CorsOptions{AllowCredentials: allowCredentials, MaxAge: maxAge}
	if lower == "*" || lower == "<origin>" {
		o.AllowOrigins = lower
	} else {
		o.Origins = SplitByComma(StringValue(lower))
	}
	return o
}

func CorsOptionsFromEnv() CorsOptions {
	return NewCorsOptions(
		CorsAllowOrigin.Load().AsString(),
		CorsAllowCredentials.Load().AsBool(),
		CorsMaxAge.Load().AsInt(),
	)
}

// AllowOriginFor returns the Access-Control-Allow-Origin value for a request origin, or "" when none applies.
func (o CorsOptions) AllowOriginFor(requestOrigin string) string {
	if o.AllowOrigins == "*" {
		return "*"
	}
	if o.AllowOrigins == "<origin>" {
		return requestOrigin
	}
	for _, allowed := range o.Origins {
		if allowed == requestOrigin {
			return requestOrigin
		}
	}
	return ""
}
