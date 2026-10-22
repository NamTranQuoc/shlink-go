// Package cli ports upstream's module/CLI/test-cli and checks the console output of every command against goldens
// captured from upstream.
package cli

import (
	"os"
	"testing"
)

// notPorted lists the commands whose bodies the port does not implement yet. Tests running them are skipped against
// the port; remove a command from here in the change that ports it.
var notPorted = map[string]bool{}

// skipIfNotPorted skips the test against the port when it runs any of the given commands' bodies.
func skipIfNotPorted(t *testing.T, commands ...string) {
	t.Helper()
	if os.Getenv("CONTRACT_TARGET") != "go" {
		return
	}
	for _, c := range commands {
		if notPorted[c] {
			t.Skipf("%s is not ported yet", c)
		}
	}
}
