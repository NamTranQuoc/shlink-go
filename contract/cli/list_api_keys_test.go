package cli

import (
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestListApiKeys_generatesExpectedOutput(t *testing.T) {
	now := time.Now().UTC()
	expiredAPIKeyDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).
		Format("2006-01-02T15:04:05-07:00")
	enabledOnlyOutput := `+--------------------+---------------------------+--------------------------+
| Name               | Expiration date           | Roles                    |
+--------------------+---------------------------+--------------------------+
| valid_api_key      | -                         | Admin                    |
+--------------------+---------------------------+--------------------------+
| expired_api_key    | ` + expiredAPIKeyDate + ` | Admin                    |
+--------------------+---------------------------+--------------------------+
| author_api_key     | -                         | Author only              |
+--------------------+---------------------------+--------------------------+
| domain_api_key     | -                         | Domain only: example.com |
+--------------------+---------------------------+--------------------------+
| no_orphans_api_key | -                         | No orphan visits         |
+--------------------+---------------------------+--------------------------+
`
	cases := []struct {
		name     string
		flags    []string
		expected string
	}{
		{"no flags", nil, `+--------------------+------------+---------------------------+--------------------------+
| Name               | Is enabled | Expiration date           | Roles                    |
+--------------------+------------+---------------------------+--------------------------+
| valid_api_key      | +++        | -                         | Admin                    |
+--------------------+------------+---------------------------+--------------------------+
| disabled_api_key   | ---        | -                         | Admin                    |
+--------------------+------------+---------------------------+--------------------------+
| expired_api_key    | ---        | ` + expiredAPIKeyDate + ` | Admin                    |
+--------------------+------------+---------------------------+--------------------------+
| author_api_key     | +++        | -                         | Author only              |
+--------------------+------------+---------------------------+--------------------------+
| domain_api_key     | +++        | -                         | Domain only: example.com |
+--------------------+------------+---------------------------+--------------------------+
| no_orphans_api_key | +++        | -                         | No orphan visits         |
+--------------------+------------+---------------------------+--------------------------+
`},
		{"-e", []string{"-e"}, enabledOnlyOutput},
		{"--enabled-only", []string{"--enabled-only"}, enabledOnlyOutput},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			output, exitCode := h.Exec(t, append([]string{"api-key:list"}, c.flags...))

			h.AssertEquals(t, c.expected, output)
			h.AssertEquals(t, 0, exitCode)
		})
	}
}
