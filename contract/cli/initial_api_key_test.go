package cli

import (
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestInitialApiKey_createsNoKeyWhenOtherApiKeysAlreadyExist(t *testing.T) {
	h.Seed(t)
	output, _ := h.Exec(t, []string{"api-key:initial", "new_api_key", "-v"})

	h.AssertEquals(t, "Other API keys already exist. Initial API key creation skipped.\n", output)
}
