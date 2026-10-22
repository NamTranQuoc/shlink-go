package cli

import "github.com/NamTranQuoc/shlink-go/internal/console"

// commandDefinitions returns every Shlink command with the name, description, help and input definition upstream
// declares, in the order the config providers register them (shlink-importer first, then the CLI module).
func commandDefinitions() []*console.Command {
	return []*console.Command{
		{
			Name:        "short-url:import",
			Description: "Allows to import short URLs from third party sources",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("source", console.ArgRequired, "The source from which you want to import", nil),
				},
				nil,
			),
		},
		{
			Name:        "short-url:create",
			Description: "Generates a short URL for provided long URL and returns it",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("long-url", console.ArgRequired, "The long URL to set", nil),
				},
				[]*console.Option{
					console.NewOption("tag", "t", console.OptValueRequired|console.OptValueIsArray, "Tags to apply to the short URL", nil),
					console.NewOption("valid-since", "s", console.OptValueRequired, "The date from which this short URL will be valid. If someone tries to access it before this date, it will not be found", nil),
					console.NewOption("valid-until", "u", console.OptValueRequired, "The date until which this short URL will be valid. If someone tries to access it after this date, it will not be found", nil),
					console.NewOption("max-visits", "m", console.OptValueRequired, "This will limit the number of visits for this short URL", nil),
					console.NewOption("title", "", console.OptValueRequired, "A descriptive title for the short URL", nil),
					console.NewOption("crawlable", "r", console.OptValueNegatable, "Tells if this short URL will be included as \"Allow\" in Shlink's robots.txt", nil),
					console.NewOption("no-forward-query", "w", console.OptValueNegatable, "Disables the forwarding of the query string to the long URL, when the short URL is visited", nil),
					console.NewOption("domain", "d", console.OptValueRequired, "The domain to which this short URL will be attached", nil),
					console.NewOption("custom-slug", "c", console.OptValueRequired, "If provided, this slug will be used instead of generating a short code", nil),
					console.NewOption("short-code-length", "l", console.OptValueRequired, "The length for generated short code (it will be ignored if --custom-slug was provided)", nil),
					console.NewOption("path-prefix", "p", console.OptValueRequired, "Prefix to prepend before the generated short code or provided custom slug", nil),
					console.NewOption("find-if-exists", "f", console.OptValueNone, "This will force existing matching URL to be returned if found, instead of creating a new one", nil),
				},
			),
		},
		{
			Name:        "short-url:edit",
			Description: "Edit an existing short URL",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code to edit", nil),
				},
				[]*console.Option{
					console.NewOption("tag", "t", console.OptValueRequired|console.OptValueIsArray, "Tags to apply to the short URL", nil),
					console.NewOption("valid-since", "s", console.OptValueRequired, "The date from which this short URL will be valid. If someone tries to access it before this date, it will not be found", nil),
					console.NewOption("valid-until", "u", console.OptValueRequired, "The date until which this short URL will be valid. If someone tries to access it after this date, it will not be found", nil),
					console.NewOption("max-visits", "m", console.OptValueRequired, "This will limit the number of visits for this short URL", nil),
					console.NewOption("title", "", console.OptValueRequired, "A descriptive title for the short URL", nil),
					console.NewOption("crawlable", "r", console.OptValueNegatable, "Tells if this short URL will be included as \"Allow\" in Shlink's robots.txt", nil),
					console.NewOption("no-forward-query", "w", console.OptValueNegatable, "Disables the forwarding of the query string to the long URL, when the short URL is visited", nil),
					console.NewOption("domain", "d", console.OptValueRequired, "The domain to which the short URL is attached", nil),
					console.NewOption("long-url", "l", console.OptValueRequired, "The long URL to set", nil),
				},
			),
		},
		{
			Name:        "short-url:resolve",
			Description: "Returns the long URL behind a short code",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code to resolve", nil),
				},
				[]*console.Option{
					console.NewOption("domain", "d", console.OptValueRequired, "The domain to which the short URL is attached", nil),
				},
			),
		},
		{
			Name:        "short-url:list",
			Description: "List all short URLs",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("page", "p", console.OptValueRequired, "The first page to list (10 items per page unless \"--all\" is provided).", 1),
					console.NewOption("all", "", console.OptValueNone, "Disables pagination and just displays all existing URLs. Caution! If the amount of short URLs is big,this may end up failing due to memory usage.", nil),
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return short URLs older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return short URLs newer than this date", nil),
					console.NewOption("search-term", "st", console.OptValueRequired, "A query used to filter results by searching for it on the longUrl and shortCode fields", nil),
					console.NewOption("domain", "d", console.OptValueRequired, "Used to filter results by domain. Use DEFAULT keyword to filter by default domain", nil),
					console.NewOption("tag", "t", console.OptValueRequired|console.OptValueIsArray, "A list of tags that short URLs need to include", nil),
					console.NewOption("tags-all", "", console.OptValueNone, "If --tag is provided, returns only short URLs including ALL of them", nil),
					console.NewOption("exclude-tag", "et", console.OptValueRequired|console.OptValueIsArray, "A list of tags that short URLs should NOT include", nil),
					console.NewOption("exclude-tags-all", "", console.OptValueNone, "If --exclude-tag is provided, returns only short URLs not including ANY of them", nil),
					console.NewOption("exclude-max-visits-reached", "", console.OptValueNone, "Excludes short URLs which reached their max amount of visits", nil),
					console.NewOption("exclude-past-valid-until", "", console.OptValueNone, "Excludes short URLs which have a \"validUntil\" date in the past", nil),
					console.NewOption("order-by", "o", console.OptValueRequired, "Field name to order the list by. Set the dir by optionally passing ASC or DESC after \"-\": --orderBy=tags-ASC", nil),
					console.NewOption("api-key-name", "kn", console.OptValueRequired, "List only short URLs created by the API key matching provided name", nil),
					console.NewOption("show-tags", "", console.OptValueNone, "Whether to display the tags or not", nil),
					console.NewOption("show-domain", "", console.OptValueNone, "Whether to display the domain or not. Those belonging to default domain will have value \"DEFAULT\"", nil),
					console.NewOption("show-api-key", "k", console.OptValueNone, "Whether to display the API key name from which the URL was generated or not", nil),
				},
			),
		},
		{
			Name:        "short-url:visits",
			Description: "Returns the detailed visits information for provided short code",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code which visits we want to get", nil),
				},
				[]*console.Option{
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return visits older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return visits newer than this date", nil),
					console.NewOption("format", "f", console.OptValueRequired, "Output format (\"full\", \"paginated\" or \"csv\")", "full"),
					console.NewOption("domain", "d", console.OptValueRequired, "The domain for the short code", nil),
				},
			),
		},
		{
			Name:        "short-url:delete",
			Description: "Deletes a short URL",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code for the short URL to be deleted", nil),
				},
				[]*console.Option{
					console.NewOption("domain", "d", console.OptValueRequired, "The domain if the short code does not belong to the default one", nil),
					console.NewOption("ignore-threshold", "i", console.OptValueNone, "Ignores the safety visits threshold check, which could make short URLs with many visits to be accidentally deleted", nil),
				},
			),
		},
		{
			Name:        "short-url:visits-delete",
			Description: "Deletes visits from a short URL",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code for the short URL which visits will be deleted", nil),
				},
				[]*console.Option{
					console.NewOption("domain", "d", console.OptValueRequired, "The domain if the short code does not belong to the default one", nil),
				},
			),
		},
		{
			Name:        "short-url:delete-expired",
			Description: "Deletes all short URLs that are considered expired, because they have a validUntil date in the past",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("evaluate-max-visits", "", console.OptValueNone, "Also take into consideration short URLs which have reached their max amount of visits.", nil),
					console.NewOption("force", "f", console.OptValueNone, "Delete short URLs with no confirmation", nil),
					console.NewOption("dry-run", "", console.OptValueNone, "Only check how many short URLs would be affected, without actually deleting them", nil),
				},
			),
		},
		{
			Name:        "visit:locate",
			Description: "Resolves visits origin locations. It implicitly downloads/updates the GeoLite2 db file if needed",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("retry", "r", console.OptValueNone, "Will retry the location of visits that were located with a not-found location, in case it was due to a temporal issue.", nil),
					console.NewOption("all", "a", console.OptValueNone, "When provided together with --retry, will locate all existing visits, regardless the fact that they have already been located.", nil),
				},
			),
		},
		{
			Name:        "visit:download-db",
			Description: "Checks if the GeoLite2 db file is too old or it does not exist, and tries to download an up-to-date copy if so",
		},
		{
			Name:        "visit:orphan",
			Description: "Returns the list of orphan visits",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return visits older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return visits newer than this date", nil),
					console.NewOption("format", "f", console.OptValueRequired, "Output format (\"full\", \"paginated\" or \"csv\")", "full"),
					console.NewOption("domain", "d", console.OptValueRequired, "Return visits that belong to this domain only. Use DEFAULT keyword for visits in default domain", nil),
					console.NewOption("type", "t", console.OptValueRequired, "Return visits only with this type", nil),
				},
			),
		},
		{
			Name:        "visit:orphan-delete",
			Description: "Deletes all orphan visits",
		},
		{
			Name:        "visit:non-orphan",
			Description: "Returns the list of non-orphan visits",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return visits older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return visits newer than this date", nil),
					console.NewOption("format", "f", console.OptValueRequired, "Output format (\"full\", \"paginated\" or \"csv\")", "full"),
					console.NewOption("domain", "d", console.OptValueRequired, "Return visits that belong to this domain only. Use DEFAULT keyword for visits in default domain", nil),
				},
			),
		},
		{
			Name:        "api-key:generate",
			Description: "Generate a new valid API key",
			Help:        "The <info>%command.name%</info> generates a new valid API key.\n\n    <info>%command.full_name%</info>\n\nYou can optionally set its name for tracking purposes with <comment>--name</comment> or <comment>-m</comment>:\n\n    <info>%command.full_name% --name Alice</info>\n\nYou can optionally set its expiration date with <comment>--expiration-date</comment> or <comment>-e</comment>:\n\n    <info>%command.full_name% --expiration-date 2020-01-01</info>",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("name", "m", console.OptValueRequired, "The unique name by which this API key will be known", nil),
					console.NewOption("expiration-date", "e", console.OptValueRequired, "The date in which the API key should expire. Use any valid PHP format", nil),
					console.NewOption("author-only", "a", console.OptValueNone, "Adds the \"AUTHORED_SHORT_URLS\" role to the new API key", nil),
					console.NewOption("domain-only", "d", console.OptValueRequired, "Adds the \"DOMAIN_SPECIFIC\" role to the new API key, with provided domain", nil),
					console.NewOption("no-orphan-visits", "o", console.OptValueNone, "Adds the \"NO_ORPHAN_VISITS\" role to the new API key", nil),
				},
			),
		},
		{
			Name:        "api-key:disable",
			Description: "Disables an API key by name",
			Help:        "The <info>%command.name%</info> command allows you to disable an existing API key.\n\nIf no arguments are provided, you will be prompted to select one of the existing non-disabled API keys.\n\n    <info>%command.full_name%</info>\n\nYou can optionally pass the API key name to be disabled:\n\n    <info>%command.full_name% the_key_name</info>\n",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("name", console.ArgOptional, "The name of the API key to disable.", nil),
				},
				nil,
			),
		},
		{
			Name:        "api-key:delete",
			Description: "Deletes an API key by name",
			Help:        "The <info>%command.name%</info> command allows you to delete an existing API key via its name.\n\nIf no arguments are provided, you will be prompted to select one of the existing API keys.\n\n    <info>%command.full_name%</info>\n\nYou can optionally pass the API key name to be disabled:\n\n    <info>%command.full_name% the_key_name</info>\n",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("name", console.ArgOptional, "The API key to delete.", nil),
				},
				nil,
			),
		},
		{
			Name:        "api-key:list",
			Description: "Lists all the available API keys.",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("enabled-only", "e", console.OptValueNone, "Tells if only enabled API keys should be returned.", nil),
				},
			),
		},
		{
			Name:        "api-key:initial",
			Description: "Tries to create initial API key",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("api-key", console.ArgRequired, "The initial API to create", nil),
				},
				nil,
			),
		},
		{
			Name:        "api-key:rename",
			Description: "Renames an API key by name",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("old-name", console.ArgRequired, "Current name of the API key to rename", nil),
					console.NewArgument("new-name", console.ArgRequired, "New name to set to the API key", nil),
				},
				nil,
			),
		},
		{
			Name:        "tag:list",
			Description: "Lists existing tags.",
		},
		{
			Name:        "tag:rename",
			Description: "Renames one existing tag.",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("old-name", console.ArgRequired, "Current name of the tag.", nil),
					console.NewArgument("new-name", console.ArgRequired, "New name of the tag.", nil),
				},
				nil,
			),
		},
		{
			Name:        "tag:delete",
			Description: "Deletes one or more tags.",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("name", "t", console.OptValueRequired|console.OptValueIsArray, "The name of the tags to delete", nil),
				},
			),
		},
		{
			Name:        "tag:visits",
			Description: "Returns the list of visits for provided tag",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("tag", console.ArgRequired, "The tag which visits we want to get", nil),
				},
				[]*console.Option{
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return visits older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return visits newer than this date", nil),
					console.NewOption("format", "f", console.OptValueRequired, "Output format (\"full\", \"paginated\" or \"csv\")", "full"),
					console.NewOption("domain", "d", console.OptValueRequired, "Return visits that belong to this domain only. Use DEFAULT keyword for visits in default domain", nil),
				},
			),
		},
		{
			Name:        "domain:list",
			Description: "List all domains that have been ever used for some short URL",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("show-redirects", "r", console.OptValueNone, "Will display an extra column with the information of the \"not found\" redirects for every domain.", nil),
				},
			),
		},
		{
			Name:        "domain:redirects",
			Description: "Set specific \"not found\" redirects for individual domains.",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("domain", console.ArgRequired, "The domain authority to which you want to set the specific redirects", nil),
				},
				nil,
			),
		},
		{
			Name:        "domain:visits",
			Description: "Returns the list of visits for provided domain",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("domain", console.ArgRequired, "The domain which visits we want to get", nil),
				},
				[]*console.Option{
					console.NewOption("start-date", "s", console.OptValueRequired, "Only return visits older than this date", nil),
					console.NewOption("end-date", "e", console.OptValueRequired, "Only return visits newer than this date", nil),
					console.NewOption("format", "f", console.OptValueRequired, "Output format (\"full\", \"paginated\" or \"csv\")", "full"),
				},
			),
		},
		{
			Name:        "db:create",
			Description: "Creates the database needed for shlink to work. It will do nothing if the database already exists",
			Hidden:      true,
		},
		{
			Name:        "db:migrate",
			Description: "Runs database migrations, which will ensure the shlink database is up to date",
			Hidden:      true,
		},
		{
			Name:        "short-url:manage-rules",
			Description: "Set redirect rules for a short URL",
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("short-code", console.ArgRequired, "The short code which rules we want to set", nil),
				},
				[]*console.Option{
					console.NewOption("domain", "d", console.OptValueRequired, "The domain of the short code", nil),
				},
			),
		},
		{
			Name:        "integration:matomo:send-visits",
			Description: "Send existing visits to the configured matomo instance",
			Help:        "This command allows you to send existing visits from this Shlink instance to the configured Matomo server.\n\nIts intention is to allow you to configure Matomo at some point in time, and still have your whole visits \nhistory tracked there.\n\nThis command will unconditionally send to Matomo all visits for a specific date range, so make sure you \nprovide the proper limits to avoid duplicated visits.\n\nSend all visits created so far:\n    <info>%command.name%</info>\n\nSend all visits created before 2024:\n    <info>%command.name% --until 2023-12-31</info>\n\nSend all visits created after a specific day:\n    <info>%command.name% --since 2022-03-27</info>\n\nSend all visits created during 2022:\n    <info>%command.name% --since 2022-01-01 --until 2022-12-31</info>",
			Definition: console.NewDefinition(
				nil,
				[]*console.Option{
					console.NewOption("since", "s", console.OptValueRequired, "Only visits created since this date, inclusively, will be sent to Matomo", nil),
					console.NewOption("until", "u", console.OptValueRequired, "Only visits created until this date, inclusively, will be sent to Matomo", nil),
				},
			),
		},
		{
			Name:        "env-var:read",
			Description: "Display current value for an env var",
			Hidden:      true,
			Definition: console.NewDefinition(
				[]*console.Argument{
					console.NewArgument("env-var", console.ArgRequired, "The env var to read", nil),
				},
				nil,
			),
		},
	}
}
