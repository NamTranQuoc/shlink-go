package cli

import (
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestManageRedirectRules_printsErrorsWhenPassingInvalidValues(t *testing.T) {
	skipIfNotPorted(t, "short-url:manage-rules")
	h.Seed(t)
	output, _ := h.Exec(t, []string{"short-url:manage-rules", "abc123"},
		"0",                   // Add new rule
		"not-a-number",        // Invalid priority
		"1",                   // Valid priority, to continue execution
		"invalid-long-url",    // Invalid long URL
		"https://example.com", // Valid long URL, to continue execution
		"1",                   // Language condition type
		"",                    // Invalid required language
		"es-ES",               // Valid language, to continue execution
		"no",                  // Do not add more conditions
		"4",                   // Discard changes
	)

	assertContains(t, output, "The priority must be a numeric positive value")
	assertContains(t, output, "Provided value does not match a loose URI pattern")
	assertContains(t, output, "The value is mandatory")
}
