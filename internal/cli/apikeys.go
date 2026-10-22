package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

func init() {
	register("api-key:generate", Body{Execute: generateAPIKey})
	register("api-key:disable", Body{Interact: chooseAPIKey("What API key do you want to disable?", true), Execute: disableAPIKey})
	register("api-key:delete", Body{Interact: chooseAPIKey("What API key do you want to delete?", false), Execute: deleteAPIKey})
	register("api-key:list", Body{Execute: listAPIKeys})
	register("api-key:initial", Body{Execute: initialAPIKey})
	register("api-key:rename", Body{
		Interact: func(env *Env, io *console.IO) error {
			if err := askArgument(io, "old-name", "What API key do you want to rename?"); err != nil {
				return err
			}
			return askArgument(io, "new-name", "What is the new name you want to set?")
		},
		Execute: renameAPIKey,
	})
}

// askArgument ports the #[Ask] interaction of a non-nullable string argument: the question is only asked when the
// argument is missing, and an empty answer leaves it missing.
func askArgument(io *console.IO, name, question string) error {
	if v := io.Input.Argument(name); v != nil {
		if list, isList := v.([]any); !isList || len(list) > 0 {
			return nil
		}
	}
	answer, err := io.Style().AskQuestion(console.NewQuestion(question, nil))
	if err != nil || answer == nil {
		return err
	}
	io.Input.SetArgument(name, answer)
	return nil
}

// determineRoles ports RoleResolver::determineRoles().
func determineRoles(env *Env, s *Services, in *console.Input) ([]core.RoleDefinition, error) {
	var roles []core.RoleDefinition
	if in.OptionBool("author-only") {
		roles = append(roles, core.RoleForAuthoredShortURLs())
	}
	if authority := in.OptionString("domain-only"); authority != nil {
		if *authority == s.URLs.DefaultDomain {
			return nil, &console.PHPError{Class: `Shlinkio\Shlink\CLI\Exception\InvalidRoleConfigException`,
				Message: `You cannot create an API key with the "DOMAIN_SPECIFIC" role attached to the default domain. ` +
					"The role is currently limited to non-default domains.",
				File: "InvalidRoleConfigException.php", Line: 16}
		}
		d, err := s.Domains.GetOrCreate(env.Ctx, *authority, nil)
		if err != nil {
			return nil, err
		}
		roles = append(roles, core.RoleForDomain(d))
	}
	if in.OptionBool("no-orphan-visits") {
		roles = append(roles, core.RoleForNoOrphanVisits())
	}
	return roles, nil
}

func generateAPIKey(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	expiration, err := normalizeOptionalDate(io.Input.OptionString("expiration-date"))
	if err != nil {
		return 0, err
	}
	roles, err := determineRoles(env, s, io.Input)
	if err != nil {
		return 0, err
	}
	meta := core.NewAPIKeyMeta(nil, io.Input.OptionString("name"), expiration, roles)
	key, err := s.APIKeys.Create(env.Ctx, meta)
	if err != nil {
		return 0, err
	}
	style.Success(fmt.Sprintf(`Generated API key: "%s"`, meta.Key))
	if io.Input.IsInteractive() {
		style.Warning("Save the key in a secure location. You will not be able to get it afterwards.")
	}
	if !core.IsAdmin(key) {
		var rows [][]any
		for _, r := range key.Roles {
			rows = append(rows, []any{string(r.Role), arrayToString(roleMetaKeys(r.Meta), r.Meta, 0)})
		}
		shlinkTable(style, false, []any{"Role name", "Role metadata"}, rows, "", "Roles")
	}
	return console.Success, nil
}

// chooseAPIKey ports the interact() of DisableKeyCommand and DeleteKeyCommand.
func chooseAPIKey(question string, enabledOnly bool) func(env *Env, io *console.IO) error {
	return func(env *Env, io *console.IO) error {
		if io.Input.Argument("name") != nil {
			return nil
		}
		s, err := env.Services()
		if err != nil {
			return err
		}
		keys, err := s.APIKeys.ListKeys(env.Ctx, enabledOnly)
		if err != nil {
			return err
		}
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = k.Name
		}
		name, err := io.Style().Choice(question, names, nil)
		if err != nil {
			return err
		}
		io.Input.SetArgument("name", name)
		return nil
	}
}

func disableAPIKey(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	name := io.Input.ArgumentString("name")
	if name == nil {
		style.Warning("An API key name was not provided.")
		return console.Invalid, nil
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	// Upstream only catches shlink-common's InvalidArgumentException here, which disableByName() never throws, so a
	// missing key ends up rendered as an ApiKeyNotFoundException
	if _, err := s.APIKeys.DisableByName(env.Ctx, *name); err != nil {
		return 0, err
	}
	style.Success(fmt.Sprintf(`API key "%s" properly disabled`, *name))
	return console.Success, nil
}

func deleteAPIKey(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	name := io.Input.ArgumentString("name")
	if name == nil {
		style.Warning("An API key name was not provided.")
		return console.Invalid, nil
	}
	if io.Input.IsInteractive() {
		style.Warning("You are about to delete an API key. This action cannot be undone.")
		proceed, err := style.Confirm("Are you sure you want to delete the API key?", true)
		if err != nil {
			return 0, err
		}
		if !proceed {
			return console.Invalid, nil
		}
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	if err := s.APIKeys.DeleteByName(env.Ctx, *name); err != nil {
		var notFound core.APIKeyNotFoundError
		if errors.As(err, &notFound) {
			style.Error(err.Error())
			return console.Failure, nil
		}
		return 0, err
	}
	style.Success(fmt.Sprintf(`API key "%s" properly deleted`, *name))
	return console.Success, nil
}

func listAPIKeys(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	enabledOnly := io.Input.OptionBool("enabled-only")
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	keys, err := s.APIKeys.ListKeys(env.Ctx, enabledOnly)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	rows := make([][]any, 0, len(keys))
	for _, k := range keys {
		pattern := "<info>%s</info>"
		if !k.Enabled {
			pattern = "<fg=red>%s</>"
		} else if k.IsExpired(now) {
			pattern = "<comment>%s</comment>"
		}
		name := k.Name
		if name == "" {
			name = "-"
		}
		row := []any{fmt.Sprintf(pattern, name)}
		if !enabledOnly {
			symbol := "+++"
			if !k.Enabled || k.IsExpired(now) {
				symbol = "---"
			}
			row = append(row, fmt.Sprintf(pattern, symbol))
		}
		expiration := "-"
		if k.ExpirationDate != nil {
			expiration = phpx.AtomString(*k.ExpirationDate)
		}
		row = append(row, expiration)
		if core.IsAdmin(k) {
			row = append(row, "Admin")
		} else {
			names := make([]string, len(k.Roles))
			for i, r := range k.Roles {
				names[i] = r.FriendlyName()
			}
			row = append(row, strings.Join(names, "\n"))
		}
		rows = append(rows, row)
	}
	headers := []any{"Name"}
	if !enabledOnly {
		headers = append(headers, "Is enabled")
	}
	headers = append(headers, "Expiration date", "Roles")
	shlinkTable(style, true, headers, rows, "", "")
	return console.Success, nil
}

func initialAPIKey(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	key, err := s.APIKeys.CreateInitial(env.Ctx, deref(io.Input.ArgumentString("api-key"), ""))
	if err != nil {
		return 0, err
	}
	if key == nil && style.IsVerbose() {
		style.Writeln("<comment>Other API keys already exist. Initial API key creation skipped.</comment>", console.OutputNormal)
	}
	return console.Success, nil
}

func renameAPIKey(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	r := core.Renaming{OldName: deref(io.Input.ArgumentString("old-name"), ""), NewName: deref(io.Input.ArgumentString("new-name"), "")}
	if _, err := s.APIKeys.RenameAPIKey(env.Ctx, r); err != nil {
		return 0, err
	}
	style.Success("API key properly renamed")
	return console.Success, nil
}
