package harness

import "testing"

// knownUpstreamFailures lists tests that upstream Shlink itself fails on a given engine, with the reason. They are
// skipped for every target on that engine: parity means the port passes everything upstream passes.
var knownUpstreamFailures = map[string]map[string]string{
	"sqlite": {
		"TestListShortUrls_shortUrlsAreProperlyListed/#4": "two rows share short code ghi789; SQL leaves their order " +
			"undefined and SQLite returns them reversed (upstream only runs API tests on PostgreSQL)",
	},
	"mysql": {
		"TestImportGoldens/import-shlink": "the imported URL's tags are loaded without ORDER BY; this engine returns them " +
			"in a different order than the SQLite capture",
	},
	"maria": {
		"TestImportGoldens/import-shlink": "the imported URL's tags are loaded without ORDER BY; this engine returns them " +
			"in a different order than the SQLite capture",
	},
	"mssql": {
		"TestImportGoldens/import-shlink": "the imported URL's tags are loaded without ORDER BY; this engine returns them " +
			"in a different order than the SQLite capture",
		"TestListShortUrls_shortUrlsAreProperlyListed/#4": "two rows share short code ghi789; SQL leaves their order " +
			"undefined and SQL Server returns them reversed",
		"TestListShortUrls_shortUrlsAreProperlyListed/#20": "abc123 and ghi789 share the same creation date; their " +
			"order under the default dateCreated ordering is undefined",
		"TestListShortUrls_shortUrlsAreProperlyListed/#23": "rows with equal creation dates come back in an undefined " +
			"order",
		"TestListShortUrls_generatesExpectedOutput/default_domain": "abc123 and ghi789 share the same creation date; " +
			"their order under the default dateCreated ordering is undefined",
	},
}

func skipKnownUpstreamFailure(t testing.TB) {
	t.Helper()
	if reason, ok := knownUpstreamFailures[LoadConfig().DBDriver][t.Name()]; ok {
		t.Skipf("known upstream failure on %s: %s", LoadConfig().DBDriver, reason)
	}
}
