// Package config loads Shlink's configuration from environment variables, with the same parsing rules, defaults and
// `_FILE` fallback upstream applies (module/Core/src/Config/EnvVars.php and shlink-config's env()).
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvVar is the name of a supported environment variable.
type EnvVar string

const (
	AppEnv                         EnvVar = "APP_ENV"
	DeleteShortURLThreshold        EnvVar = "DELETE_SHORT_URL_THRESHOLD"
	DBDriver                       EnvVar = "DB_DRIVER"
	DBName                         EnvVar = "DB_NAME"
	DBUser                         EnvVar = "DB_USER"
	DBPassword                     EnvVar = "DB_PASSWORD"
	DBHost                         EnvVar = "DB_HOST"
	DBUnixSocket                   EnvVar = "DB_UNIX_SOCKET"
	DBPort                         EnvVar = "DB_PORT"
	DBUseEncryption                EnvVar = "DB_USE_ENCRYPTION"
	GeoliteLicenseKey              EnvVar = "GEOLITE_LICENSE_KEY"
	CacheNamespace                 EnvVar = "CACHE_NAMESPACE"
	RedisServers                   EnvVar = "REDIS_SERVERS"
	RedisSentinelService           EnvVar = "REDIS_SENTINEL_SERVICE"
	RedisServersUser               EnvVar = "REDIS_SERVERS_USER"
	RedisServersPassword           EnvVar = "REDIS_SERVERS_PASSWORD"
	RedisPubSubEnabled             EnvVar = "REDIS_PUB_SUB_ENABLED"
	MercureEnabled                 EnvVar = "MERCURE_ENABLED"
	MercurePublicHubURL            EnvVar = "MERCURE_PUBLIC_HUB_URL"
	MercureInternalHubURL          EnvVar = "MERCURE_INTERNAL_HUB_URL"
	MercureJWTSecret               EnvVar = "MERCURE_JWT_SECRET"
	RabbitMQEnabled                EnvVar = "RABBITMQ_ENABLED"
	RabbitMQHost                   EnvVar = "RABBITMQ_HOST"
	RabbitMQPort                   EnvVar = "RABBITMQ_PORT"
	RabbitMQUser                   EnvVar = "RABBITMQ_USER"
	RabbitMQPassword               EnvVar = "RABBITMQ_PASSWORD"
	RabbitMQVhost                  EnvVar = "RABBITMQ_VHOST"
	RabbitMQUseSSL                 EnvVar = "RABBITMQ_USE_SSL"
	MatomoEnabled                  EnvVar = "MATOMO_ENABLED"
	MatomoBaseURL                  EnvVar = "MATOMO_BASE_URL"
	MatomoSiteID                   EnvVar = "MATOMO_SITE_ID"
	MatomoAPIToken                 EnvVar = "MATOMO_API_TOKEN"
	DefaultInvalidShortURLRedirect EnvVar = "DEFAULT_INVALID_SHORT_URL_REDIRECT"
	DefaultRegular404Redirect      EnvVar = "DEFAULT_REGULAR_404_REDIRECT"
	DefaultBaseURLRedirect         EnvVar = "DEFAULT_BASE_URL_REDIRECT"
	RedirectStatusCode             EnvVar = "REDIRECT_STATUS_CODE"
	RedirectCacheLifetime          EnvVar = "REDIRECT_CACHE_LIFETIME"
	RedirectCacheVisibility        EnvVar = "REDIRECT_CACHE_VISIBILITY"
	BasePath                       EnvVar = "BASE_PATH"
	ShortURLTrailingSlash          EnvVar = "SHORT_URL_TRAILING_SLASH"
	ShortURLModeVar                EnvVar = "SHORT_URL_MODE"
	AnonymizeRemoteAddr            EnvVar = "ANONYMIZE_REMOTE_ADDR"
	TrackOrphanVisits              EnvVar = "TRACK_ORPHAN_VISITS"
	DisableTrackParam              EnvVar = "DISABLE_TRACK_PARAM"
	DisableTracking                EnvVar = "DISABLE_TRACKING"
	DisableIPTracking              EnvVar = "DISABLE_IP_TRACKING"
	DisableReferrerTracking        EnvVar = "DISABLE_REFERRER_TRACKING"
	DisableUATracking              EnvVar = "DISABLE_UA_TRACKING"
	DisableTrackingFrom            EnvVar = "DISABLE_TRACKING_FROM"
	DefaultShortCodesLength        EnvVar = "DEFAULT_SHORT_CODES_LENGTH"
	IsHTTPSEnabled                 EnvVar = "IS_HTTPS_ENABLED"
	DefaultDomain                  EnvVar = "DEFAULT_DOMAIN"
	AutoResolveTitles              EnvVar = "AUTO_RESOLVE_TITLES"
	RedirectExtraPathMode          EnvVar = "REDIRECT_EXTRA_PATH_MODE"
	MultiSegmentSlugsEnabled       EnvVar = "MULTI_SEGMENT_SLUGS_ENABLED"
	RobotsAllowAllShortURLs        EnvVar = "ROBOTS_ALLOW_ALL_SHORT_URLS"
	RobotsUserAgents               EnvVar = "ROBOTS_USER_AGENTS"
	Timezone                       EnvVar = "TIMEZONE"
	MemoryLimit                    EnvVar = "MEMORY_LIMIT"
	InitialAPIKey                  EnvVar = "INITIAL_API_KEY"
	SkipInitialGeoliteDownload     EnvVar = "SKIP_INITIAL_GEOLITE_DOWNLOAD"
	RealTimeUpdatesTopics          EnvVar = "REAL_TIME_UPDATES_TOPICS"
	CorsAllowOrigin                EnvVar = "CORS_ALLOW_ORIGIN"
	CorsAllowCredentials           EnvVar = "CORS_ALLOW_CREDENTIALS"
	CorsMaxAge                     EnvVar = "CORS_MAX_AGE"
	TrustedProxies                 EnvVar = "TRUSTED_PROXIES"
	LogsFormat                     EnvVar = "LOGS_FORMAT"
)

// AllEnvVars lists every supported env var, in upstream's declaration order.
var AllEnvVars = []EnvVar{
	AppEnv, DeleteShortURLThreshold, DBDriver, DBName, DBUser, DBPassword, DBHost, DBUnixSocket, DBPort,
	DBUseEncryption, GeoliteLicenseKey, CacheNamespace, RedisServers, RedisSentinelService, RedisServersUser,
	RedisServersPassword, RedisPubSubEnabled, MercureEnabled, MercurePublicHubURL, MercureInternalHubURL,
	MercureJWTSecret, RabbitMQEnabled, RabbitMQHost, RabbitMQPort, RabbitMQUser, RabbitMQPassword, RabbitMQVhost,
	RabbitMQUseSSL, MatomoEnabled, MatomoBaseURL, MatomoSiteID, MatomoAPIToken, DefaultInvalidShortURLRedirect,
	DefaultRegular404Redirect, DefaultBaseURLRedirect, RedirectStatusCode, RedirectCacheLifetime,
	RedirectCacheVisibility, BasePath, ShortURLTrailingSlash, ShortURLModeVar, AnonymizeRemoteAddr, TrackOrphanVisits,
	DisableTrackParam, DisableTracking, DisableIPTracking, DisableReferrerTracking, DisableUATracking,
	DisableTrackingFrom, DefaultShortCodesLength, IsHTTPSEnabled, DefaultDomain, AutoResolveTitles,
	RedirectExtraPathMode, MultiSegmentSlugsEnabled, RobotsAllowAllShortURLs, RobotsUserAgents, Timezone, MemoryLimit,
	InitialAPIKey, SkipInitialGeoliteDownload, RealTimeUpdatesTopics, CorsAllowOrigin, CorsAllowCredentials,
	CorsMaxAge, TrustedProxies, LogsFormat,
}

// IsValid tells if the name is a supported env var.
func IsValid(name string) bool {
	for _, v := range AllEnvVars {
		if string(v) == name {
			return true
		}
	}
	return false
}

// Kind is the dynamic type of a parsed env var value. PHP's env() returns string|int|bool|null.
type Kind int

const (
	KindNull Kind = iota
	KindString
	KindInt
	KindBool
)

// Value is a parsed env var value with PHP-like casting helpers.
type Value struct {
	Kind Kind
	Str  string
	Int  int
	Bool bool
}

var Null = Value{}

func StringValue(s string) Value { return Value{Kind: KindString, Str: s} }
func IntValue(i int) Value       { return Value{Kind: KindInt, Int: i} }
func BoolValue(b bool) Value     { return Value{Kind: KindBool, Bool: b} }

// IsNull tells if the value is PHP null.
func (v Value) IsNull() bool { return v.Kind == KindNull }

// AsBool behaves like PHP's (bool) cast.
func (v Value) AsBool() bool {
	switch v.Kind {
	case KindBool:
		return v.Bool
	case KindInt:
		return v.Int != 0
	case KindString:
		return v.Str != "" && v.Str != "0"
	}
	return false
}

// AsInt behaves like PHP's (int) cast.
func (v Value) AsInt() int {
	switch v.Kind {
	case KindBool:
		if v.Bool {
			return 1
		}
		return 0
	case KindInt:
		return v.Int
	case KindString:
		return phpIntCast(v.Str)
	}
	return 0
}

// AsString behaves like PHP's (string) cast.
func (v Value) AsString() string {
	switch v.Kind {
	case KindBool:
		if v.Bool {
			return "1"
		}
		return ""
	case KindInt:
		return strconv.Itoa(v.Int)
	case KindString:
		return v.Str
	}
	return ""
}

// StringOrNil returns nil for null and a pointer to the string representation otherwise.
func (v Value) StringOrNil() *string {
	if v.IsNull() {
		return nil
	}
	s := v.AsString()
	return &s
}

// Lookup returns the raw value of an environment variable. It is a variable so tests can replace it.
var Lookup = os.LookupEnv

// Env behaves like shlink-config's env(): unset → fallback, otherwise parseEnvVar().
func Env(key string, fallback Value) Value {
	raw, ok := Lookup(key)
	if !ok {
		return fallback
	}
	return ParseEnvVar(raw)
}

// ParseEnvVar ports shlink-config's parseEnvVar().
func ParseEnvVar(value string) Value {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "true", "(true)":
		return BoolValue(true)
	case "false", "(false)":
		return BoolValue(false)
	case "empty", "(empty)":
		return StringValue("")
	case "null", "(null)":
		return Null
	}
	if isNumeric(trimmed) {
		return IntValue(phpIntCast(trimmed))
	}
	return StringValue(trimmed)
}

// Load ports EnvVars::loadFromEnv(): the env var, then the `_FILE` variant, then the default value.
func (e EnvVar) Load() Value {
	if v := Env(string(e), Null); !v.IsNull() {
		return v
	}
	if v := e.loadFromFile(); !v.IsNull() {
		return v
	}
	return e.defaultValue()
}

// ExistsInEnv tells if the env var resolves to a non-null value.
func (e EnvVar) ExistsInEnv() bool { return !e.Load().IsNull() }

func (e EnvVar) loadFromFile() Value {
	file := Env(string(e)+"_FILE", Null)
	if file.IsNull() {
		return Null
	}
	path := file.AsString()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Null
	}
	content, err := os.ReadFile(path)
	// PHP's `$content ? ... : null` treats "" and "0" as falsy
	if err != nil || len(content) == 0 || string(content) == "0" {
		return Null
	}
	return ParseEnvVar(string(content))
}

const (
	DefaultDeleteShortURLThreshold = 15
	DefaultShortCodesLengthValue   = 5
	MinShortCodesLength            = 4
	DefaultRedirectCacheLifetime   = 30
	DefaultRedirectCacheVisibility = "private"
)

func (e EnvVar) defaultValue() Value {
	switch e {
	case AppEnv:
		return StringValue("prod")
	case MemoryLimit:
		return StringValue("512M")
	case Timezone:
		return StringValue(defaultTimezone())
	case DefaultShortCodesLength:
		return IntValue(DefaultShortCodesLengthValue)
	case ShortURLModeVar:
		return StringValue("strict")
	case IsHTTPSEnabled, AutoResolveTitles:
		return BoolValue(true)
	case MultiSegmentSlugsEnabled, ShortURLTrailingSlash:
		return BoolValue(false)
	case DefaultDomain, BasePath:
		return StringValue("")
	case CacheNamespace:
		return StringValue("Shlink")
	case RedirectExtraPathMode:
		return StringValue("default")
	case RedisPubSubEnabled, MatomoEnabled, RobotsAllowAllShortURLs:
		return BoolValue(false)
	case DBName:
		return StringValue("shlink")
	case DBHost:
		return DBUnixSocket.Load()
	case DBDriver:
		return StringValue("sqlite")
	case DBPort:
		switch DBDriver.Load().AsString() {
		case "postgres":
			return StringValue("5432")
		case "mssql":
			return StringValue("1433")
		}
		return StringValue("3306")
	case DBUseEncryption:
		return BoolValue(false)
	case MercureEnabled:
		return BoolValue(MercurePublicHubURL.ExistsInEnv())
	case MercureInternalHubURL:
		return MercurePublicHubURL.Load()
	case RabbitMQEnabled, RabbitMQUseSSL:
		return BoolValue(false)
	case RabbitMQPort:
		return IntValue(5672)
	case RabbitMQVhost:
		return StringValue("/")
	case RedirectStatusCode:
		return IntValue(302)
	case RedirectCacheLifetime:
		return IntValue(DefaultRedirectCacheLifetime)
	case AnonymizeRemoteAddr, TrackOrphanVisits:
		return BoolValue(true)
	case DisableTracking, DisableIPTracking, DisableReferrerTracking, DisableUATracking:
		return BoolValue(false)
	case CorsAllowOrigin:
		return StringValue("*")
	case CorsAllowCredentials:
		return BoolValue(false)
	case CorsMaxAge:
		return IntValue(3600)
	case LogsFormat:
		return StringValue("console")
	}
	return Null
}

// defaultTimezone mirrors PHP's date_default_timezone_get() in the upstream image, which is UTC unless TZ is set.
func defaultTimezone() string {
	if tz, ok := Lookup("TZ"); ok && tz != "" {
		return tz
	}
	return "UTC"
}

// IsProdEnv, IsDevEnv and IsTestEnv port the EnvVars helpers.
func IsProdEnv() bool { return AppEnv.Load().AsString() == "prod" }
func IsDevEnv() bool  { return AppEnv.Load().AsString() == "dev" }
func IsTestEnv() bool { return AppEnv.Load().AsString() == "test" }

// Location resolves the configured TIMEZONE.
func Location() *time.Location {
	loc, err := time.LoadLocation(Timezone.Load().AsString())
	if err != nil {
		return time.UTC
	}
	return loc
}
