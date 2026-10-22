package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/console"
)

// InstallerVersion is the shlinkio/shlink-installer version upstream ships.
const InstallerVersion = "v10.1.0"

// installationCommand ports one entry of the installer's installation_commands config, merged with Shlink's
// overrides. A nil args runs nothing; an empty args succeeds without running anything, for the Doctrine ORM
// commands the port has no counterpart for (proxies, metadata cache).
type installationCommand struct {
	// command is the command upstream runs, shown in verbose mode
	command      string
	args         []string
	initMessage  string
	errorMessage string
	failOnError  bool
	printOutput  bool
	timeout      time.Duration
}

const phpBinary = "/usr/local/bin/php"

var installationCommands = map[string]installationCommand{
	"db_create_schema": {command: "bin/cli db:create", args: []string{"db:create"}, initMessage: "Initializing database if needed...",
		errorMessage: "Error generating database.", failOnError: true, timeout: 600 * time.Second},
	"db_migrate": {command: "bin/cli db:migrate", args: []string{"db:migrate"}, initMessage: "Updating database...",
		errorMessage: "Error updating database.", failOnError: true, timeout: 600 * time.Second},
	"orm_proxies": {command: "bin/doctrine orm:generate-proxies", args: []string{}, initMessage: "Generating proxies...",
		errorMessage: "Error generating proxies.", failOnError: true},
	"orm_clear_cache": {command: "bin/doctrine orm:clear-cache:metadata", args: []string{},
		initMessage: "Clearing entities cache...", errorMessage: "Error clearing entities cache."},
	"geolite_download_db": {command: "bin/cli visit:download-db", args: []string{"visit:download-db"},
		initMessage: "Downloading GeoLite2 db file...", errorMessage: "Error downloading GeoLite2 db."},
	"api_key_generate": {command: "bin/cli api-key:generate", args: []string{"api-key:generate"},
		initMessage: "Generating first API key...", errorMessage: "Error generating first API key.", printOutput: true},
	"api_key_create": {command: "bin/cli api-key:initial", args: []string{"api-key:initial"},
		initMessage: "Creating first API key...", errorMessage: "Error creating first API key.", printOutput: true},
	"road_runner_update": {command: "vendor/bin/rr get --no-interaction --no-config --location bin/",
		initMessage: "Downloading RoadRunner binary...", errorMessage: "Error downloading RoadRunner binary."},
}

// NewInstallerApplication builds the shlink-installer console application. cli is the executable running Shlink's
// own commands.
func NewInstallerApplication(cli string) *console.Application {
	app := console.NewApplication("Shlink installer", InstallerVersion)
	app.Add(&console.Command{
		Name: "init",
		Description: "Initializes external dependencies required for Shlink to properly work, like DB, cache warmup, " +
			"initial GeoLite DB download, etc",
		Definition: console.NewDefinition(nil, []*console.Option{
			console.NewOption("skip-initialize-db", "", console.OptValueNone, "Skip the initial empty database creation. "+
				"It will make this command fail on a later stage if the database was not created manually", nil),
			console.NewOption("clear-db-cache", "", console.OptValueNone, "Clear the database metadata cache", nil),
			console.NewOption("initial-api-key", "", console.OptValueOptional, "Create an initial API key. A random one "+
				"will be generated and printed if no value is provided", false),
			console.NewOption("download-rr-binary", "", console.OptValueNone, "Download a RoadRunner binary. Useful only "+
				"if you plan to serve Shlink with Roadrunner", nil),
			console.NewOption("skip-download-geolite", "", console.OptValueNone, "Skip downloading the initial GeoLite DB "+
				"file. Shlink will try to download it the first time it needs to geolocate visits", nil),
		}),
		Execute: func(io *console.IO) (int, error) { return initCommand(io, cli) },
	})
	app.Add(&console.Command{
		Name:        "install",
		Description: "Guides you through the installation process, to get Shlink up and running",
		Execute: func(io *console.IO) (int, error) {
			return runInstallation(io, io.Style(), map[string]any{"--initial-api-key": nil}, importedConfig{})
		},
	})
	app.Add(&console.Command{
		Name:        "set-option",
		Description: "Allows you to set new values for any config option",
		Execute:     setOptionCommand,
	})
	app.Add(&console.Command{
		Name:        "update",
		Description: "Helps you import Shlink's config from an older version to a new one",
		Execute:     updateCommand,
	})
	return app
}

// initCommand ports InitCommand with InitCommandInput::resolveCommands().
func initCommand(io *console.IO, cli string) (int, error) {
	style := io.Style()
	in := io.Input
	type step struct {
		name string
		arg  *string
	}
	var steps []step
	if !in.OptionBool("skip-initialize-db") {
		steps = append(steps, step{name: "db_create_schema"})
	}
	steps = append(steps, step{name: "db_migrate"}, step{name: "orm_proxies"})
	if in.OptionBool("clear-db-cache") {
		steps = append(steps, step{name: "orm_clear_cache"})
	}
	if !in.OptionBool("skip-download-geolite") {
		steps = append(steps, step{name: "geolite_download_db"})
	}
	switch v := in.Option("initial-api-key").(type) {
	case nil:
		// The option was passed without a value
		steps = append(steps, step{name: "api_key_generate"})
	case string:
		steps = append(steps, step{name: "api_key_create", arg: &v})
	}
	if in.OptionBool("download-rr-binary") {
		steps = append(steps, step{name: "road_runner_update"})
	}
	ok := true
	for _, s := range steps {
		ok = execInstallationCommand(style, cli, installationCommands[s.name], in.IsInteractive(), s.arg) && ok
	}
	if !ok {
		return console.Failure, nil
	}
	return console.Success, nil
}

// execInstallationCommand ports InstallationCommandsRunner::execPhpCommand().
func execInstallationCommand(io *console.SymfonyStyle, cli string, c installationCommand, interactive bool, arg *string) bool {
	io.Write(c.initMessage, false, console.OutputNormal)
	command := phpBinary + " " + c.command
	args := c.args
	if arg != nil {
		command += " " + *arg
		args = append(append([]string{}, args...), *arg)
	}
	io.Write(fmt.Sprintf(` <options=bold>[Running "%s"]</> `, command), false, console.VerbosityVerbose)

	success, output := true, ""
	switch {
	case args == nil:
		// The RoadRunner binary is not used by this port
		success = false
	case len(args) > 0:
		success, output = runInstallationProcess(cli, args, c.timeout)
	}
	isWarning := !success && !c.failOnError
	verbosityIndicator := "Set SHELL_VERBOSITY=3"
	if interactive {
		verbosityIndicator = "Run with -vvv"
	}
	switch {
	case success:
		io.Writeln(" <info>Success!</info>", console.OutputNormal)
	case isWarning:
		io.Write(" <comment>Warning!</comment>", false, console.OutputNormal)
		if io.IsVerbose() {
			io.Writeln("", console.OutputNormal)
		} else {
			io.Writeln(fmt.Sprintf(" %s to see error.", verbosityIndicator), console.OutputNormal)
		}
	case !io.IsVerbose():
		io.Error(fmt.Sprintf("%s. %s to see specific error info.", c.errorMessage, verbosityIndicator))
	}
	if c.printOutput {
		io.Text(output)
	}
	return success || isWarning
}

// runInstallationProcess runs one of Shlink's commands in a child process, like Symfony's Process with no input.
func runInstallationProcess(cli string, args []string, timeout time.Duration) (bool, string) {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Env = os.Environ()
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	return err == nil, out.String()
}
