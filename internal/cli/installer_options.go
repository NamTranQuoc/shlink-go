package cli

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// configOption ports the installer's ConfigOptionInterface for the options Shlink enables.
type configOption struct {
	envVar string
	// shouldBeAsked is called after BaseConfigOption::shouldBeAsked() passes; nil asks always
	shouldBeAsked func(answers *phpx.Array) (bool, error)
	ask           func(io *console.SymfonyStyle, answers *phpx.Array) (any, error)
}

// configOptionGroup is one titled group of options, in the order ConfigGenerator asks them.
type configOptionGroup struct {
	title   string
	options []string
}

// configOptionGroups are the installer's option groups (config_options.groups), every option enabled by Shlink, in
// the order ConfigGenerator::resolveAndSortOptions() leaves them.
var configOptionGroups = []configOptionGroup{
	{"SERVER", []string{"RUNTIME", "MEMORY_LIMIT", "LOGS_FORMAT"}},
	{"DATABASE", []string{"DB_DRIVER", "DB_NAME", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_UNIX_SOCKET",
		"DB_USE_ENCRYPTION"}},
	{"URL SHORTENER", []string{"DEFAULT_DOMAIN", "IS_HTTPS_ENABLED", "DEFAULT_SHORT_CODES_LENGTH", "AUTO_RESOLVE_TITLES",
		"REDIRECT_EXTRA_PATH_MODE", "MULTI_SEGMENT_SLUGS_ENABLED", "SHORT_URL_TRAILING_SLASH", "SHORT_URL_MODE",
		"GEOLITE_LICENSE_KEY", "REDIRECT_STATUS_CODE", "REDIRECT_CACHE_LIFETIME", "REDIRECT_CACHE_VISIBILITY"}},
	{"TRACKING", []string{"DISABLE_TRACKING", "TRACK_ORPHAN_VISITS", "DISABLE_TRACK_PARAM", "DISABLE_TRACKING_FROM",
		"DISABLE_IP_TRACKING", "ANONYMIZE_REMOTE_ADDR", "DISABLE_UA_TRACKING", "DISABLE_REFERRER_TRACKING"}},
	{"REDIRECTS", []string{"DEFAULT_BASE_URL_REDIRECT", "DEFAULT_INVALID_SHORT_URL_REDIRECT",
		"DEFAULT_REGULAR_404_REDIRECT"}},
	{"ROBOTS", []string{"ROBOTS_ALLOW_ALL_SHORT_URLS", "ROBOTS_USER_AGENTS"}},
	{"CORS", []string{"CORS_ALLOW_ORIGIN", "CORS_ALLOW_CREDENTIALS", "CORS_MAX_AGE"}},
	{"APPLICATION", []string{"DELETE_SHORT_URL_THRESHOLD", "BASE_PATH", "TIMEZONE", "CACHE_NAMESPACE",
		"TRUSTED_PROXIES"}},
	{"INTEGRATIONS", []string{"REDIS_SERVERS", "REDIS_SENTINEL_SERVICE", "REDIS_SERVERS_USER", "REDIS_SERVERS_PASSWORD",
		"REDIS_PUB_SUB_ENABLED", "MERCURE_ENABLED", "MERCURE_PUBLIC_HUB_URL", "MERCURE_INTERNAL_HUB_URL",
		"MERCURE_JWT_SECRET", "RABBITMQ_ENABLED", "RABBITMQ_HOST", "RABBITMQ_USE_SSL", "RABBITMQ_PORT", "RABBITMQ_USER",
		"RABBITMQ_PASSWORD", "RABBITMQ_VHOST", "MATOMO_ENABLED", "MATOMO_BASE_URL", "MATOMO_SITE_ID",
		"MATOMO_API_TOKEN", "REAL_TIME_UPDATES_TOPICS"}},
}

// setOptionChoices are the options set-option offers (the groups' keyed options), in config order.
var setOptionChoices = []struct{ title, envVar string }{
	{"Memory limit", "MEMORY_LIMIT"},
	{"Logs format", "LOGS_FORMAT"},
	{"Database > Driver", "DB_DRIVER"},
	{"Database > Name", "DB_NAME"},
	{"Database > Host (or unix socket for PostgreSQL)", "DB_HOST"},
	{"Database > Port", "DB_PORT"},
	{"Database > User", "DB_USER"},
	{"Database > Password", "DB_PASSWORD"},
	{"Database > Unix socket (Mysql/MariaDB)", "DB_UNIX_SOCKET"},
	{"Database > Use encryption", "DB_USE_ENCRYPTION"},
	{"URL shortener > Short domain", "DEFAULT_DOMAIN"},
	{"URL shortener > Schema", "IS_HTTPS_ENABLED"},
	{"URL shortener > Short codes length", "DEFAULT_SHORT_CODES_LENGTH"},
	{"URL shortener > Auto resolve titles", "AUTO_RESOLVE_TITLES"},
	{"URL shortener > Extra path mode", "REDIRECT_EXTRA_PATH_MODE"},
	{"URL shortener > Multi-segment slugs", "MULTI_SEGMENT_SLUGS_ENABLED"},
	{"URL shortener > Trailing slashes", "SHORT_URL_TRAILING_SLASH"},
	{"URL shortener > Mode", "SHORT_URL_MODE"},
	{"GeoLite2 license key", "GEOLITE_LICENSE_KEY"},
	{"Redirects > Status code (301/302/307/308)", "REDIRECT_STATUS_CODE"},
	{"Redirects > Caching life time", "REDIRECT_CACHE_LIFETIME"},
	{"Redirects > Caching visibility", "REDIRECT_CACHE_VISIBILITY"},
	{"Tracking > Orphan visits tracking", "TRACK_ORPHAN_VISITS"},
	{"Tracking > Param to disable tracking", "DISABLE_TRACK_PARAM"},
	{"Tracking > Disabled IP addresses", "DISABLE_TRACKING_FROM"},
	{"Tracking > Disable tracking", "DISABLE_TRACKING"},
	{"Tracking > Disable IP address tracking", "DISABLE_IP_TRACKING"},
	{"Tracking > IP Anonymization", "ANONYMIZE_REMOTE_ADDR"},
	{"Tracking > Disable user agent tracking", "DISABLE_UA_TRACKING"},
	{"Tracking > Disable referrer tracking", "DISABLE_REFERRER_TRACKING"},
	{"Redirects > Base URL", "DEFAULT_BASE_URL_REDIRECT"},
	{"Redirects > Invalid short URL", "DEFAULT_INVALID_SHORT_URL_REDIRECT"},
	{"Redirects > Regular 404", "DEFAULT_REGULAR_404_REDIRECT"},
	{"Robots.txt > allow all", "ROBOTS_ALLOW_ALL_SHORT_URLS"},
	{"Robots.txt > user agents", "ROBOTS_USER_AGENTS"},
	{"CORS > Allow origin", "CORS_ALLOW_ORIGIN"},
	{"CORS > Allow credentials", "CORS_ALLOW_CREDENTIALS"},
	{"CORS > Max age", "CORS_MAX_AGE"},
	{"Delete short URLs > Visits threshold", "DELETE_SHORT_URL_THRESHOLD"},
	{"Base path", "BASE_PATH"},
	{"Timezone", "TIMEZONE"},
	{"Cache > namespace", "CACHE_NAMESPACE"},
	{"Trusted proxies", "TRUSTED_PROXIES"},
	{"Redis > servers", "REDIS_SERVERS"},
	{"Redis > sentinels service", "REDIS_SENTINEL_SERVICE"},
	{"Redis > servers user", "REDIS_SERVERS_USER"},
	{"Redis > servers password", "REDIS_SERVERS_PASSWORD"},
	{"Redis > Pub/sub enabled", "REDIS_PUB_SUB_ENABLED"},
	{"Mercure > Public URL", "MERCURE_PUBLIC_HUB_URL"},
	{"Mercure > Internal URL", "MERCURE_INTERNAL_HUB_URL"},
	{"Mercure > JWT Secret", "MERCURE_JWT_SECRET"},
	{"RabbitMQ > Enable", "RABBITMQ_ENABLED"},
	{"RabbitMQ > Host", "RABBITMQ_HOST"},
	{"RabbitMQ > SSL", "RABBITMQ_USE_SSL"},
	{"RabbitMQ > Port", "RABBITMQ_PORT"},
	{"RabbitMQ > User", "RABBITMQ_USER"},
	{"RabbitMQ > Password", "RABBITMQ_PASSWORD"},
	{"RabbitMQ > VHost", "RABBITMQ_VHOST"},
	{"Matomo > Enable", "MATOMO_ENABLED"},
	{"Matomo > Server URL", "MATOMO_BASE_URL"},
	{"Matomo > Site ID", "MATOMO_SITE_ID"},
	{"Matomo > API token", "MATOMO_API_TOKEN"},
	{"Real-time updates > topics", "REAL_TIME_UPDATES_TOPICS"},
}

// configOptions are the installer's config options by env var.
var configOptions = map[string]*configOption{}

func init() {
	for _, o := range []*configOption{
		// Server
		{envVar: "RUNTIME", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			runtimes := []string{"RoadRunner", "Classic web server (Nginx, Apache, etc)"}
			runtime, err := io.Choice("Select the runtime you are planning to use to serve Shlink (this is only used to "+
				"conditionally skip some follow-up questions)", runtimes, &runtimes[0])
			if runtime == runtimes[0] {
				return "async", err
			}
			return "regular", err
		}},
		{envVar: "MEMORY_LIMIT", ask: askWith("What is the maximum amount of RAM every process run by Shlink should be "+
			"allowed to use? (Provide a number for bytes, a number followed by K for kilobytes, M for Megabytes or G for "+
			"Gigabytes)", strPtr("512M"), validateMemoryValue)},
		{envVar: "LOGS_FORMAT", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			return io.Choice("In what format do you want Shlink to generate logs?", []string{"console", "json"},
				strPtr("console"))
		}},

		// Database
		{envVar: "DB_DRIVER", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			databases := []string{"MySQL", "MariaDB", "PostgreSQL", "MicrosoftSQL",
				"SQLite [<options=bold;fg=yellow>Not supported for production</>]"}
			drivers := []string{"mysql", "mysql", "postgres", "mssql", "sqlite"}
			db, err := io.Choice("Select database type", databases, &databases[0])
			for i, d := range databases {
				if d == db {
					return drivers[i], err
				}
			}
			return nil, err
		}},
		{envVar: "DB_NAME", shouldBeAsked: nonSqlite, ask: askWith("Database name", strPtr("shlink"), nil)},
		{envVar: "DB_HOST", shouldBeAsked: nonSqlite, ask: func(io *console.SymfonyStyle, answers *phpx.Array) (any, error) {
			extra := ""
			if dbDriver(answers) == "postgres" {
				extra = " (or unix socket)"
			}
			return io.Ask("Database host"+extra, strPtr("localhost"), nil)
		}},
		{envVar: "DB_PORT", shouldBeAsked: nonSqlite, ask: func(io *console.SymfonyStyle, answers *phpx.Array) (any, error) {
			port := map[string]string{"mysql": "3306", "postgres": "5432", "mssql": "1433"}[dbDriver(answers)]
			return io.Ask("Database port", &port, nil)
		}},
		{envVar: "DB_USER", shouldBeAsked: nonSqlite, ask: askWith("Database username", nil, validateRequired("username"))},
		{envVar: "DB_PASSWORD", shouldBeAsked: nonSqlite, ask: askWith("Database password", nil,
			validateRequired("password"))},
		{envVar: "DB_UNIX_SOCKET", shouldBeAsked: func(answers *phpx.Array) (bool, error) {
			return dbDriver(answers) == "mysql", nil
		}, ask: askWith("Unix socket (leave empty to not use a socket)", nil, nil)},
		{envVar: "DB_USE_ENCRYPTION", shouldBeAsked: nonSqlite, ask: confirmWith("Do you want the database connection to be "+
			"encrypted? Enabling this will make database connections fail if your database server does not support or "+
			"enforce encryption.", false)},

		// URL shortener
		{envVar: "DEFAULT_DOMAIN", ask: askWith("Default domain for generated short URLs", nil, validateRequired("domain"))},
		{envVar: "IS_HTTPS_ENABLED", ask: confirmWith("Is HTTPS enabled on this server?", true)},
		{envVar: "DEFAULT_SHORT_CODES_LENGTH", ask: askWith("What is the default length you want generated short codes to "+
			"have? (You will still be able to override this on every created short URL)", strPtr("5"),
			validateNumberGreaterThan(4))},
		{envVar: "AUTO_RESOLVE_TITLES", ask: confirmWith("Do you want Shlink to resolve the short URL title based on the "+
			"long URL's title tag (if any)? Otherwise, it will be kept empty unless explicitly provided.", true)},
		{envVar: "REDIRECT_EXTRA_PATH_MODE", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			return io.AskQuestion(console.NewAssocChoiceQuestion("Do you want Shlink to redirect short URLs as soon as "+
				"the first segment of the path matches a short code?\n\n"+
				"  append:\n"+
				"    * {shortDomain}/{shortCode}/[...extraPath] -> {longUrl}/[...extraPath]\n"+
				"    * https://s.test/abc123                    -> https://www.example.com\n"+
				"    * https://s.test/abc123/shlinkio           -> https://www.example.com/shlinkio\n\n"+
				"  ignore:\n"+
				"    * {shortDomain}/{shortCode}/[...extraPath] -> {longUrl}\n"+
				"    * https://s.test/abc123                    -> https://www.example.com\n"+
				"    * https://s.test/abc123/shlinkio           -> https://www.example.com\n\n",
				[]string{"default", "append", "ignore"},
				[]string{"Match strictly", "Append extra path", "Discard extra path"}, "default"))
		}},
		{envVar: "MULTI_SEGMENT_SLUGS_ENABLED", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			answer, err := io.AskQuestion(console.NewAssocChoiceQuestion("Do you want to support short URLs with "+
				"multi-segment custom slugs? (for example, https://example.com/foo/bar)", []string{"yes", "no"},
				[]string{"Custom slugs will support multiple segments (https://example.com/foo/bar/baz). Orphan visits " +
					"will only have either \"base_url\" or \"invalid_short_url\" type.",
					"Slugs and short codes will support only one segment (https://example.com/foo). Orphan visits will " +
						"have one of \"base_url\", \"invalid_short_url\" or \"regular_404\" type."}, "no"))
			return answer == "yes", err
		}},
		{envVar: "SHORT_URL_TRAILING_SLASH", ask: confirmWith("Do you want to support trailing slashes in short URLs? "+
			"(https://s.test/foo and https://s.test/foo/ will be considered the same)", false)},
		{envVar: "SHORT_URL_MODE", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			return io.AskQuestion(console.NewAssocChoiceQuestion("How do you want short URLs to be matched?\n"+
				"<options=bold;fg=yellow> Warning!</> <comment>This feature is experimental. It only applies to public "+
				"routes (short URLs and QR codes). REST API routes always use strict match.</comment>\n",
				[]string{"strict", "loose"},
				[]string{"Short codes and custom slugs will be matched in a case-sensitive way (\"foo\" !== \"FOO\"). " +
					"Generated short codes will include lowercase letters, uppercase letters and numbers.",
					"Short codes and custom slugs will be matched in a case-insensitive way (\"foo\" === \"FOO\"). " +
						"Generated short codes will include only lowercase letters and numbers."}, "strict"))
		}},
		{envVar: "GEOLITE_LICENSE_KEY", ask: askWith("Provide a GeoLite2 license key. Leave empty to disable geolocation. "+
			"(Go to https://shlink.io/documentation/geolite-license-key to know how to generate it)", nil, nil)},
		{envVar: "REDIRECT_STATUS_CODE", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			statuses := []string{"302", "301", "307", "308"}
			descriptions := []string{
				"All visits will always be tracked. Not that good for SEO. Only GET requests will be redirected.",
				"Best option for SEO. Redirect will be cached for a short period of time, making some visits not to be " +
					"tracked. Only GET requests will be redirected.",
				"Same as 302, but Shlink will also redirect on non-GET requests.",
				"Same as 301, but Shlink will also redirect on non-GET requests.",
			}
			answer, err := io.AskQuestion(console.NewKeyedChoiceQuestion(
				"What kind of redirect do you want your short URLs to have?", statuses, descriptions, "302"))
			for i, d := range descriptions {
				if d == answer {
					status, _ := strconv.Atoi(statuses[i])
					return status, err
				}
			}
			return nil, err
		}},
		{envVar: "REDIRECT_CACHE_LIFETIME", shouldBeAsked: permanentRedirect, ask: askWith("How long (in seconds) do you "+
			"want your redirects to be cached by visitors?", strPtr("30"), validatePositiveNumber)},
		{envVar: "REDIRECT_CACHE_VISIBILITY", shouldBeAsked: permanentRedirect, ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			public, err := io.Confirm("Do you want redirects to be cached by reverse proxies?", false)
			if public {
				return "public", err
			}
			return "private", err
		}},

		// Tracking
		{envVar: "DISABLE_TRACKING", ask: confirmWith("Do you want to completely disable visits tracking?", false)},
		{envVar: "TRACK_ORPHAN_VISITS", shouldBeAsked: trackingEnabled, ask: confirmWith("Do you want track orphan visits? "+
			"(visits to the base URL, invalid short URLs or other \"not found\" URLs)", true)},
		{envVar: "DISABLE_TRACK_PARAM", shouldBeAsked: trackingEnabled, ask: askWith("Provide a parameter name that you will "+
			"be able to use to disable tracking on specific request to short URLs (leave empty and this feature won't be "+
			"enabled)", nil, nil)},
		{envVar: "DISABLE_TRACKING_FROM", shouldBeAsked: trackingEnabled, ask: askWith("Provide a comma-separated list of IP "+
			"addresses, CIDR blocks or wildcard addresses (1.2.*.*) from which you want tracking to be disabled", nil, nil)},
		{envVar: "DISABLE_IP_TRACKING", shouldBeAsked: trackingEnabled, ask: confirmWith("Do you want to disable tracking "+
			"of visitors' IP addresses?", false)},
		{envVar: "ANONYMIZE_REMOTE_ADDR", shouldBeAsked: func(answers *phpx.Array) (bool, error) {
			return !phpx.Truthy(answers.Get("DISABLE_TRACKING")) && !phpx.Truthy(answers.Get("DISABLE_IP_TRACKING")), nil
		}, ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			anonymize, err := io.Confirm("Do you want visitors' remote IP addresses to be anonymized before persisting "+
				"them to the database?", true)
			if err != nil || anonymize {
				return true, err
			}
			io.Warning("Careful! If you disable IP address anonymization, you will no longer be in compliance with the " +
				"GDPR and other similar data protection regulations.")
			disable, err := io.Confirm("Do you still want to disable anonymization?", false)
			return !disable, err
		}},
		{envVar: "DISABLE_UA_TRACKING", shouldBeAsked: trackingEnabled, ask: confirmWith("Do you want to disable tracking "+
			"of visitors' \"User Agents\"?", false)},
		{envVar: "DISABLE_REFERRER_TRACKING", shouldBeAsked: trackingEnabled, ask: confirmWith("Do you want to disable "+
			"tracking of visitors' \"Referrers\"?", false)},

		// Redirects
		{envVar: "DEFAULT_BASE_URL_REDIRECT", ask: askWith("Custom URL to redirect to when a user hits Shlink's base URL "+
			"(If no value is provided, the user will see a default \"404 not found\" page)", nil, validateURL)},
		{envVar: "DEFAULT_INVALID_SHORT_URL_REDIRECT", ask: askWith("Custom URL to redirect to when a user hits an invalid "+
			"short URL (If no value is provided, the user will see a default \"404 not found\" page)", nil, validateURL)},
		{envVar: "DEFAULT_REGULAR_404_REDIRECT", ask: askWith("Custom URL to redirect to when a user hits a not found URL "+
			"other than an invalid short URL (If no value is provided, the user will see a default \"404 not found\" "+
			"page)", nil, validateURL)},

		// Robots
		{envVar: "ROBOTS_ALLOW_ALL_SHORT_URLS", ask: confirmWith("Do you want all short URLs to be crawlable/allowed by the "+
			"robots.txt file? You can still allow them individually, regardless of this.", false)},
		{envVar: "ROBOTS_USER_AGENTS", ask: askWith("Provide a comma-separated list of user agents for your robots.txt "+
			"file. Defaults to all user agents (*)", nil, nil)},

		// CORS
		{envVar: "CORS_ALLOW_ORIGIN", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			answer, err := io.AskQuestion(console.NewAssocChoiceQuestion("How do you want Shlink to determine which "+
				"origins are allowed for CORS requests?", []string{"*", "<origin>", "allowlist"}, []string{
				"All hosts are implicitly allowed (Access-Control-Allow-Origin is set to \"*\")",
				"All hosts are explicitly allowed (Access-Control-Allow-Origin is set to the value in request's " +
					"Origin header)",
				"Provide a list of hosts that are allowed",
			}, "*"))
			if err != nil || answer != "allowlist" {
				return answer, err
			}
			return io.Ask("Provide a comma-separated list of origins that should be allowed to perform CORS requests "+
				"to Shlink", nil, nil)
		}},
		{envVar: "CORS_ALLOW_CREDENTIALS", ask: confirmWith("Do you want browsers to forward credentials to your Shlink "+
			"server during CORS requests?", false)},
		{envVar: "CORS_MAX_AGE", ask: askWith("How long (in seconds) do you want CORS config to be cached by browsers?",
			strPtr("3600"), validatePositiveNumber)},

		// Application
		{envVar: "DELETE_SHORT_URL_THRESHOLD", ask: askWith("What is the amount of visits from which the system will not "+
			"allow short URLs to be deleted? Leave empty to always allow deleting short URLs, no matter what", nil,
			func(value any) (any, error) {
				if value == nil {
					return nil, nil
				}
				return validatePositiveNumber(value)
			})},
		{envVar: "BASE_PATH", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			path, err := io.Ask("What is the path from which shlink is going to be served? (It must include a leading "+
				"bar, like \"/shlink\". Leave empty if you plan to serve shlink from the root of the domain)", nil, nil)
			if path == nil {
				path = ""
			}
			return path, err
		}},
		{envVar: "TIMEZONE", ask: askWith("Set the timezone in which your Shlink instance is running (leave empty to use "+
			"the one set in PHP config)", nil, nil)},
		{envVar: "CACHE_NAMESPACE", ask: askWith("Prefix for cache entry keys. (Change this if you run multiple Shlink "+
			"instances on this server, or they share the same redis instance)", strPtr("Shlink"), nil)},
		{envVar: "TRUSTED_PROXIES", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			proxies, err := io.Confirm("Do you have more than one proxy in front of this Shlink instance?", false)
			if err != nil || !proxies {
				return nil, err
			}
			option, err := io.AskQuestion(console.NewAssocChoiceQuestion("How do you want your proxies IP addresses to "+
				"be identified, so that the visitor IP address can be properly determined?", []string{"amount", "list"},
				[]string{"Just set the amount of proxies", "Define a comma-separated list of IP addresses, CIDR blocks " +
					"or wildcard addresses (1.2.*.*)"}, "list"))
			if err != nil {
				return nil, err
			}
			if option == "amount" {
				return io.Ask("How many proxies do you have in front of Shlink?", nil, validatePositiveNumber)
			}
			return io.Ask("Provide a comma-separated list of your proxies' IP addresses, CIDR blocks or wildcard "+
				"addresses (1.2.*.*)", nil, validateRequired("trusted proxies"))
		}},

		// Integrations
		{envVar: "REDIS_SERVERS", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			useRedis, err := io.Confirm("Do you want to use a redis instance, redis cluster or redis sentinels as a "+
				"shared cache for Shlink? (recommended if you run a cluster of Shlink instances)", false)
			if err != nil || !useRedis {
				return nil, err
			}
			return io.Ask("Provide a comma-separated list of URIs (redis servers/sentinel instances). If they contains "+
				"credentials with URL-reserved chars, make sure they are URL-encoded", nil, nil)
		}},
		{envVar: "REDIS_SENTINEL_SERVICE", shouldBeAsked: isSet("REDIS_SERVERS"), ask: askWith("Provide the name of the "+
			"sentinel service (leave empty if not using redis sentinel)", nil, nil)},
		{envVar: "REDIS_SERVERS_USER", shouldBeAsked: isSet("REDIS_SENTINEL_SERVICE"), ask: askWith("Provide a username "+
			"for your redis connection (leave empty if ACL is not required)", nil, nil)},
		{envVar: "REDIS_SERVERS_PASSWORD", shouldBeAsked: isSet("REDIS_SENTINEL_SERVICE"), ask: askWith("Provide a "+
			"password for your redis connection (leave empty if ACL is not required)", nil, nil)},
		{envVar: "REDIS_PUB_SUB_ENABLED", shouldBeAsked: isSet("REDIS_SERVERS"), ask: confirmWith("Do you want Shlink to "+
			"publish real-time updates in this Redis instance/cluster?", false)},
		{envVar: "MERCURE_ENABLED", shouldBeAsked: asyncRuntime, ask: confirmWith("Do you want Shlink to publish "+
			"real-time updates in a Mercure hub server?", false)},
		{envVar: "MERCURE_PUBLIC_HUB_URL", shouldBeAsked: asyncAndEnabled("MERCURE_ENABLED"), ask: askWith("Public URL of "+
			"the mercure hub server", nil, validateRequired("public hub URL"))},
		{envVar: "MERCURE_INTERNAL_HUB_URL", shouldBeAsked: asyncAndEnabled("MERCURE_ENABLED"), ask: askWith("Internal URL "+
			"of the mercure hub server (leave empty to use the public one)", nil, nil)},
		{envVar: "MERCURE_JWT_SECRET", shouldBeAsked: asyncAndEnabled("MERCURE_ENABLED"), ask: askWith("The secret key "+
			"known by the mercure hub server to validate JWTs", nil, validateRequired("JWT secret"))},
		{envVar: "RABBITMQ_ENABLED", shouldBeAsked: asyncRuntime, ask: confirmWith("Do you want Shlink to publish "+
			"real-time updates in a RabbitMQ instance?", false)},
		{envVar: "RABBITMQ_HOST", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: askWith("RabbitMQ host name",
			nil, validateRequired("RabbitMQ host name"))},
		{envVar: "RABBITMQ_USE_SSL", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: confirmWith("Should use SSL?",
			false)},
		{envVar: "RABBITMQ_PORT", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: func(io *console.SymfonyStyle, answers *phpx.Array) (any, error) {
			port := "5672"
			if phpx.Truthy(answers.Get("RABBITMQ_USE_SSL")) {
				port = "5671"
			}
			return io.Ask("RabbitMQ port", &port, validateNumberBetween(1, 65_535))
		}},
		{envVar: "RABBITMQ_USER", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: askWith("RabbitMQ username", nil,
			validateRequired("RabbitMQ username"))},
		{envVar: "RABBITMQ_PASSWORD", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: askWith("RabbitMQ password",
			nil, validateRequired("RabbitMQ password"))},
		{envVar: "RABBITMQ_VHOST", shouldBeAsked: asyncAndEnabled("RABBITMQ_ENABLED"), ask: askWith("RabbitMQ VHost",
			strPtr("/"), nil)},
		{envVar: "MATOMO_ENABLED", ask: confirmWith("Do you want Shlink to send all visits to an external Matomo server?",
			false)},
		{envVar: "MATOMO_BASE_URL", shouldBeAsked: enabled("MATOMO_ENABLED"), ask: askWith("Matomo server URL", nil,
			validateRequired("Matomo server URL"))},
		{envVar: "MATOMO_SITE_ID", shouldBeAsked: enabled("MATOMO_ENABLED"), ask: askWith("Matomo site ID", nil,
			validateRequired("Matomo site ID"))},
		{envVar: "MATOMO_API_TOKEN", shouldBeAsked: enabled("MATOMO_ENABLED"), ask: askWith("Matomo API token", nil,
			validateRequired("Matomo API token"))},
		{envVar: "REAL_TIME_UPDATES_TOPICS", ask: func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) {
			individual, err := io.Confirm("Do you want to enable individual real-time updates topics? (All topics will "+
				"be enabled otherwise)", false)
			if err != nil || !individual {
				return nil, err
			}
			topics := phpx.NewArray()
			for _, t := range []struct{ topic, question string }{
				{"NEW_VISIT", "Notify general non-orphan visits?"},
				{"NEW_SHORT_URL_VISIT", "Notify short-url visits? (A different topic per short URL will be created)"},
				{"NEW_ORPHAN_VISIT", "Notify orphan visits?"},
				{"NEW_SHORT_URL", "Notify created short URLs?"},
			} {
				notify, err := io.Confirm(t.question, true)
				if err != nil {
					return nil, err
				}
				if notify {
					topics.Append(t.topic)
				}
			}
			return topics, nil
		}},
	} {
		configOptions[o.envVar] = o
	}
}

func askWith(question string, def *string, validator func(any) (any, error)) func(*console.SymfonyStyle, *phpx.Array) (any, error) {
	return func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) { return io.Ask(question, def, validator) }
}

func confirmWith(question string, def bool) func(*console.SymfonyStyle, *phpx.Array) (any, error) {
	return func(io *console.SymfonyStyle, _ *phpx.Array) (any, error) { return io.Confirm(question, def) }
}

// shouldAsk ports the option's shouldBeAsked(): it is asked when it has no value yet and its dependencies allow it.
func (o *configOption) shouldAsk(answers *phpx.Array) (bool, error) {
	if answers.Has(o.envVar) {
		return false, nil
	}
	if o.shouldBeAsked == nil {
		return true, nil
	}
	return o.shouldBeAsked(answers)
}

func dbDriver(answers *phpx.Array) string { return phpx.ToString(answers.Get("DB_DRIVER")) }

// nonSqlite ports AbstractNonSqliteDependentConfigOption.
func nonSqlite(answers *phpx.Array) (bool, error) { return dbDriver(answers) != "sqlite", nil }

// permanentRedirect ports AbstractPermanentRedirectDependentConfigOption.
func permanentRedirect(answers *phpx.Array) (bool, error) {
	status, ok := answers.Get("REDIRECT_STATUS_CODE").(int)
	if !ok {
		return false, fmt.Errorf("the REDIRECT_STATUS_CODE option must be an int, %s given",
			phpx.VarExport(answers.Get("REDIRECT_STATUS_CODE")))
	}
	return status == 301 || status == 308, nil
}

// trackingEnabled ports AbstractDisableTrackingDependentConfigOption.
func trackingEnabled(answers *phpx.Array) (bool, error) {
	return !phpx.Truthy(answers.Get("DISABLE_TRACKING")), nil
}

// isSet tells if another option has a non-null value, like the Redis options do.
func isSet(envVar string) func(*phpx.Array) (bool, error) {
	return func(answers *phpx.Array) (bool, error) { return answers.Get(envVar) != nil, nil }
}

func enabled(envVar string) func(*phpx.Array) (bool, error) {
	return func(answers *phpx.Array) (bool, error) { return phpx.Truthy(answers.Get(envVar)), nil }
}

// asyncRuntime ports AbstractAsyncRuntimeDependentConfigOption.
func asyncRuntime(answers *phpx.Array) (bool, error) {
	return answers.Get("RUNTIME") != nil && phpx.ToString(answers.Get("RUNTIME")) == "async", nil
}

func asyncAndEnabled(envVar string) func(*phpx.Array) (bool, error) {
	return func(answers *phpx.Array) (bool, error) {
		async, err := asyncRuntime(answers)
		return async && phpx.Truthy(answers.Get(envVar)), err
	}
}

const configOptionsValidatorFile = "ConfigOptionsValidator.php"

func invalidConfigOption(message string, line int) error {
	return &console.PHPError{Class: `Shlinkio\Shlink\Installer\Exception\InvalidConfigOptionException`, Message: message,
		File: configOptionsValidatorFile, Line: line}
}

var httpURLPattern = regexp.MustCompile(`(?i)https?://(www\.)?[-a-zA-Z0-9@:%._+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_+.~#?&/=]*)`)

// validateURL ports ConfigOptionsValidator::validateUrl().
func validateURL(value any) (any, error) {
	if value != nil && !httpURLPattern.MatchString(phpx.ToString(value)) {
		return nil, invalidConfigOption(fmt.Sprintf(`Provided value "%s" is not a valid URL`, phpx.ToString(value)), 27)
	}
	return value, nil
}

func validatePositiveNumber(value any) (any, error) { return validateNumberGreaterThan(1)(value) }

func isNumericAnswer(value any) bool {
	switch v := value.(type) {
	case int:
		return true
	case string:
		return config.IsNumericValue(config.StringValue(v))
	}
	return false
}

func intAnswer(value any) int {
	if i, ok := value.(int); ok {
		return i
	}
	return config.PHPIntCast(phpx.ToString(value))
}

// validateNumberGreaterThan ports ConfigOptionsValidator::validateNumberGreaterThan().
func validateNumberGreaterThan(min int) func(any) (any, error) {
	return func(value any) (any, error) {
		i := intAnswer(value)
		if !isNumericAnswer(value) || i < min {
			return nil, invalidConfigOption(fmt.Sprintf(`Provided value "%s" is invalid. Expected a number greater or `+
				`equal than %d`, phpx.ToString(value), min), 49)
		}
		return i, nil
	}
}

// validateNumberBetween ports ConfigOptionsValidator::validateNumberBetween().
func validateNumberBetween(min, max int) func(any) (any, error) {
	return func(value any) (any, error) {
		i := intAnswer(value)
		if !isNumericAnswer(value) || i < min || i > max {
			return nil, invalidConfigOption(fmt.Sprintf(`Provided value "%s" is invalid. Expected a number between %d `+
				`and %d`, phpx.ToString(value), min, max), 61)
		}
		return i, nil
	}
}

var memoryValuePattern = regexp.MustCompile(`(?i)^\d+([KMG])?$`)

// validateMemoryValue ports ConfigOptionsValidator::validateMemoryValue().
func validateMemoryValue(value any) (any, error) {
	if memoryValuePattern.MatchString(phpx.ToString(value)) {
		return value, nil
	}
	return nil, invalidConfigOption("Provided value is invalid. It should be an amount in bytes (1024), or a number "+
		"followed by K, M, or G (512M, 1G)", 95)
}

// validateRequired ports ConfigOptionsValidator::validateRequired().
func validateRequired(option string) func(any) (any, error) {
	return func(value any) (any, error) {
		if !phpx.Truthy(value) {
			return nil, &console.PHPError{Class: `Shlinkio\Shlink\Installer\Exception\MissingRequiredOptionException`,
				Message: fmt.Sprintf(`The "%s" is required and can't be empty`, option),
				File:    "MissingRequiredOptionException.php", Line: 15}
		}
		return value, nil
	}
}
