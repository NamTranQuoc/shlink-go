package harness

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// CLIResult is the complete outcome of a CLI command.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ExecFull runs a CLI command like Exec, and also returns what it wrote to the standard error.
func ExecFull(t testing.TB, command []string, inputs ...string) CLIResult {
	t.Helper()
	c := LoadConfig()
	if len(c.CLI) == 0 {
		t.Fatal("SHLINK_CLI is not set")
	}
	return execCLI(t, c.CLI, command, inputs...)
}

// ExecInstaller runs a shlink-installer command, like upstream's `vendor/bin/shlink-installer` run from /etc/shlink.
// SHLINK_CLI has to run the CLI through `docker exec`, whose working dir is the image's /etc/shlink.
func ExecInstaller(t testing.TB, command []string, inputs ...string) CLIResult {
	t.Helper()
	c := LoadConfig()
	if len(c.CLI) < 4 || c.CLI[0] != "docker" || c.CLI[1] != "exec" {
		t.Skip("SHLINK_CLI does not run the CLI through docker exec")
	}
	cli := append(append([]string{}, c.CLI[:len(c.CLI)-1]...), "vendor/bin/shlink-installer")
	return execCLI(t, cli, command, inputs...)
}

func execCLI(t testing.TB, cli, command []string, inputs ...string) CLIResult {
	t.Helper()
	args := append(append([]string{}, cli[1:]...), command...)
	args = append(args, "--no-ansi")
	cmd := exec.Command(cli[0], args...)
	cmd.Stdin = strings.NewReader(strings.Join(inputs, "\n"))
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run CLI %v: %v\n%s", command, err, errOut.String())
		}
		code = exitErr.ExitCode()
	}
	return CLIResult{Stdout: out.String(), Stderr: errOut.String(), ExitCode: code}
}
