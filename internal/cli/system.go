package cli

import (
	"fmt"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/db"
)

func init() {
	register("env-var:read", Body{Interact: askEnvVar, Execute: readEnvVar})
	register("db:create", Body{Execute: createDatabase})
	register("db:migrate", Body{Execute: migrateDatabase})
}

func envVarNames() []string {
	names := make([]string, len(config.AllEnvVars))
	for i, v := range config.AllEnvVars {
		names[i] = string(v)
	}
	return names
}

// askEnvVar ports ReadEnvVarCommand::askMissing().
func askEnvVar(_ *Env, io *console.IO) error {
	envVar := io.Input.ArgumentString("env-var")
	if envVar == nil {
		chosen, err := io.Style().Choice("Select the env var to read", envVarNames(), nil)
		if err != nil {
			return err
		}
		envVar = &chosen
	}
	if !config.IsValid(*envVar) {
		return console.NewConsoleError(`Symfony\Component\Console\Exception\InvalidArgumentException`,
			fmt.Sprintf("%s is not a valid Shlink environment variable", *envVar), "ReadEnvVarCommand.php", 52)
	}
	io.Input.SetArgument("env-var", *envVar)
	return nil
}

func readEnvVar(_ *Env, io *console.IO) (int, error) {
	style := io.Style()
	name := deref(io.Input.ArgumentString("env-var"), "")
	if !config.IsValid(name) {
		// EnvVars::from() throws a ValueError, which the console application does not catch
		const file = "/etc/shlink/module/CLI/src/Command/Config/ReadEnvVarCommand.php"
		arg := phpTraceArg(name)
		return 0, &console.PHPFatalError{Output: "\nFatal error: Uncaught ValueError: " + fmt.Sprintf(
			`"%s" is not a valid backing value for enum Shlinkio\Shlink\Core\Config\EnvVars in %s:36`, name, file) + "\n" +
			"Stack trace:\n" +
			"#0 " + file + `(36): Shlinkio\Shlink\Core\Config\EnvVars::from(` + arg + ")\n" +
			"#1 " + file + `(62): Shlinkio\Shlink\CLI\Command\Config\ReadEnvVarCommand::{closure:Shlinkio\Shlink\CLI\Command\Config\ReadEnvVarCommand::__construct():36}(` + arg + ")\n" +
			`#2 [internal function]: Shlinkio\Shlink\CLI\Command\Config\ReadEnvVarCommand->__invoke(Object(Symfony\Component\Console\Style\SymfonyStyle), ` + arg + ")\n" +
			`#3 /etc/shlink/vendor/symfony/console/Command/InvokableCommand.php(65): ReflectionFunction->invoke(Object(Symfony\Component\Console\Style\SymfonyStyle), ` + arg + ")\n" +
			`#4 /etc/shlink/vendor/symfony/console/Command/Command.php(282): Symfony\Component\Console\Command\InvokableCommand->__invoke(Object(Symfony\Component\Console\Input\ArgvInput), Object(Symfony\Component\Console\Output\ConsoleOutput))` + "\n" +
			`#5 /etc/shlink/vendor/symfony/console/Application.php(1129): Symfony\Component\Console\Command\Command->run(Object(Symfony\Component\Console\Input\ArgvInput), Object(Symfony\Component\Console\Output\ConsoleOutput))` + "\n" +
			`#6 /etc/shlink/vendor/symfony/console/Application.php(380): Symfony\Component\Console\Application->doRunCommand(Object(Shlinkio\Shlink\CLI\Command\Config\ReadEnvVarCommand), Object(Symfony\Component\Console\Input\ArgvInput), Object(Symfony\Component\Console\Output\ConsoleOutput))` + "\n" +
			`#7 /etc/shlink/vendor/symfony/console/Application.php(219): Symfony\Component\Console\Application->doRun(Object(Symfony\Component\Console\Input\ArgvInput), Object(Symfony\Component\Console\Output\ConsoleOutput))` + "\n" +
			"#8 /etc/shlink/bin/cli(10): Symfony\\Component\\Console\\Application->run()\n" +
			"#9 {main}\n" +
			"  thrown in " + file + " on line 36\n"}
	}
	style.Writeln(formatEnvVarValue(config.EnvVar(name).Load()), console.OutputNormal)
	return console.Success, nil
}

// formatEnvVarValue ports Shlinkio\Shlink\Config\formatEnvVarValue().
func formatEnvVarValue(v config.Value) string {
	switch v.Kind {
	case config.KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case config.KindNull:
		return ""
	}
	return v.AsString()
}

// runProcess reproduces what PhpProcessRunner::run() prints for the doctrine subprocess upstream runs: nothing unless
// the output is very verbose, then the debug formatter's RUN/RES lines. The runner gets the SymfonyStyle, which is not
// a ConsoleOutputInterface, so they go to the standard output.
func runProcess(out console.Output, cmd []string, run func() error) error {
	cmd = append([]string{"/usr/local/bin/php"}, cmd...)
	quoted := make([]string, len(cmd))
	for i, c := range cmd {
		quoted[i] = "'" + strings.ReplaceAll(c, "'", `'\''`) + "'"
	}
	veryVerbose := out.Verbosity() >= console.VerbosityVeryVerbose
	if veryVerbose {
		out.Write(fmt.Sprintf("<bg=black> </><bg=blue;fg=white> RUN </> <fg=blue>%s</>\n",
			strings.ReplaceAll(strings.Join(quoted, " "), "<", `\<`)), false, console.OutputNormal)
	}
	if err := run(); err != nil {
		return err
	}
	if veryVerbose {
		out.Write("<bg=black> </><bg=green;fg=white> RES </> <fg=green>Command ran successfully</>\n", false, console.OutputNormal)
	}
	return nil
}

func createDatabase(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	exists, err := s.DB.AnyShlinkTableExists(env.Ctx)
	if err != nil {
		// Listing tables fails when the database does not exist yet: create it with a connection without dbname
		if err := db.CreateDatabase(env.Ctx, s.DB.Params); err != nil {
			return 0, err
		}
		exists = false
	}
	if exists {
		style.Success(`Database already exists. Run "db:migrate" command to make sure it is up to date.`)
		return console.Success, nil
	}
	style.Writeln("<fg=blue>Creating database tables...</>", console.OutputNormal)
	if err := runProcess(style, []string{"bin/doctrine", "orm:schema-tool:create", "--no-interaction"}, func() error {
		return s.DB.CreateSchema(env.Ctx)
	}); err != nil {
		return 0, err
	}
	style.Success("Database properly created!")
	return console.Success, nil
}

func migrateDatabase(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	style.Writeln("<fg=blue>Migrating database...</>", console.OutputNormal)
	if err := runProcess(style, []string{"vendor/doctrine/migrations/bin/doctrine-migrations.php", "migrations:migrate", "--no-interaction"}, func() error {
		_, err := s.DB.Migrate(env.Ctx)
		return err
	}); err != nil {
		return 0, err
	}
	style.Success("Database properly migrated!")
	return console.Success, nil
}
