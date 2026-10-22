package cli

import (
	"os/exec"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// TestImportShortUrls_defaultDomainIsIgnoredWhenExplicitlyProvided ports ImportShortUrlsTest. Upstream writes the CSV
// to a temp file the CLI can read; the target runs in a container, so the file is created inside it.
func TestImportShortUrls_defaultDomainIsIgnoredWhenExplicitlyProvided(t *testing.T) {
	skipIfNotPorted(t, "short-url:import", "short-url:list")
	h.Seed(t)
	csvFile := writeTargetFile(t, "/tmp/shlink_csv_import.csv", "longURL;tags;domain;short code;Title\n"+
		"https://shlink.io;foo,baz;s.test;testing-default-domain-import-1;\n"+
		"https://example.com;foo;s.test;testing-default-domain-import-2;")

	output, _ := h.Exec(t, []string{"short-url:import", "csv"}, csvFile, ";")

	assertContains(t, output, "https://shlink.io: Imported")
	assertContains(t, output, "https://example.com: Imported")

	listOutput1, _ := h.Exec(t, []string{"short-url:list", "--show-domain", "--search-term", "testing-default-domain-import-1"})
	assertContains(t, listOutput1, "DEFAULT")
	listOutput2, _ := h.Exec(t, []string{"short-url:list", "--show-domain", "--search-term", "testing-default-domain-import-2"})
	assertContains(t, listOutput2, "DEFAULT")
}

// writeTargetFile writes a file inside the target's container, using the container of SHLINK_CLI
// ("docker exec -i <container> shlink").
func writeTargetFile(t *testing.T, path, contents string) string {
	t.Helper()
	cli := h.LoadConfig().CLI
	if len(cli) < 4 || cli[0] != "docker" || cli[1] != "exec" {
		t.Skip("SHLINK_CLI does not run the CLI through docker exec")
	}
	container := cli[len(cli)-2]
	cmd := exec.Command("docker", "exec", "-i", container, "sh", "-c", "cat > "+path)
	cmd.Stdin = strings.NewReader(contents)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write %s in %s: %v\n%s", path, container, err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "exec", container, "rm", "-f", path).Run() })
	return path
}
