package cli

import (
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestGenerateApiKey_outputIsCorrect(t *testing.T) {
	h.Seed(t)
	output, exitCode := h.Exec(t, []string{"api-key:generate"})

	if !strings.Contains(output, "[OK] Generated API key") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	h.AssertEquals(t, 0, exitCode)
}
