package cli

import (
	"fmt"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
)

func init() {
	register("domain:list", Body{Execute: listDomains})
	register("domain:redirects", Body{Interact: askRedirectsDomain, Execute: domainRedirects})
	register("domain:visits", Body{
		Interact: func(_ *Env, io *console.IO) error {
			return askArgument(io, "domain", "For what domain do you want to get visits?")
		},
		Execute: domainVisits,
	})
}

func listDomains(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	showRedirects := io.Input.OptionBool("show-redirects")
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	domains, err := s.Domains.ListDomains(env.Ctx, nil)
	if err != nil {
		return 0, err
	}
	headers := []any{"Domain", "Is default"}
	if showRedirects {
		headers = append(headers, `"Not found" redirects`)
	}
	rows := make([][]any, len(domains))
	for i, d := range domains {
		isDefault := "No"
		if d.IsDefault {
			isDefault = "Yes"
		}
		rows[i] = []any{d.Authority, isDefault}
		if showRedirects {
			rows[i] = append(rows[i], fmt.Sprintf("* Base URL: %s\n* Regular 404: %s\n* Invalid short URL: %s",
				deref(d.Redirects.BaseURLRedirect, "N/A"), deref(d.Redirects.Regular404Redirect, "N/A"),
				deref(d.Redirects.InvalidShortURLRedirect, "N/A")))
		}
	}
	shlinkTable(style, showRedirects, headers, rows, "", "")
	return console.Success, nil
}

// askRedirectsDomain ports DomainRedirectsCommand::askDomain().
func askRedirectsDomain(env *Env, io *console.IO) error {
	if io.Input.Argument("domain") != nil {
		return nil
	}
	style := io.Style()
	askNewDomain := func() (any, error) {
		return style.Ask("Domain authority for which you want to set specific redirects", nil, nil)
	}
	s, err := env.Services()
	if err != nil {
		return err
	}
	domains, err := s.Domains.ListDomains(env.Ctx, nil)
	if err != nil {
		return err
	}
	var available []string
	for _, d := range domains {
		if !d.IsDefault {
			available = append(available, d.Authority)
		}
	}
	if len(available) == 0 {
		domain, err := askNewDomain()
		if err != nil {
			return err
		}
		io.Input.SetArgument("domain", domain)
		return nil
	}
	selected, err := style.Choice("Select the domain to configure", append(available, "<options=bold>New domain</>"), nil)
	if err != nil {
		return err
	}
	if strings.Contains(selected, "New domain") {
		domain, err := askNewDomain()
		if err != nil {
			return err
		}
		io.Input.SetArgument("domain", domain)
		return nil
	}
	io.Input.SetArgument("domain", selected)
	return nil
}

func domainRedirects(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	authority := deref(io.Input.ArgumentString("domain"), "")
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	domain, err := s.Domains.FindByAuthority(env.Ctx, authority, nil)
	if err != nil {
		return 0, err
	}
	ask := func(message string, current *string) (*string, error) {
		if current == nil {
			answer, err := style.Ask(message+" (Leave empty for no redirect)", nil, nil)
			return answerPtr(answer), err
		}
		choice, err := style.Choice(message, []string{fmt.Sprintf("Keep current one: [%s]", *current), "Set new redirect URL", "Remove redirect"}, nil)
		if err != nil {
			return nil, err
		}
		switch choice {
		case "Set new redirect URL":
			answer, err := style.Ask("New redirect URL", nil, nil)
			return answerPtr(answer), err
		case "Remove redirect":
			return nil, nil
		}
		return current, nil
	}
	var current core.NotFoundRedirects
	if domain != nil {
		current = core.NotFoundRedirects{BaseURLRedirect: domain.BaseURLRedirect, Regular404Redirect: domain.Regular404Redirect,
			InvalidShortURLRedirect: domain.InvalidShortURLRedirect}
	}
	var redirects core.NotFoundRedirects
	if redirects.BaseURLRedirect, err = ask("URL to redirect to when a user hits this domain's base URL", current.BaseURLRedirect); err != nil {
		return 0, err
	}
	if redirects.Regular404Redirect, err = ask("URL to redirect to when a user hits a not found URL other than an invalid short URL", current.Regular404Redirect); err != nil {
		return 0, err
	}
	if redirects.InvalidShortURLRedirect, err = ask("URL to redirect to when a user hits an invalid short URL", current.InvalidShortURLRedirect); err != nil {
		return 0, err
	}
	if _, err := s.Domains.ConfigureNotFoundRedirects(env.Ctx, authority, redirects, nil); err != nil {
		return 0, err
	}
	style.Success(fmt.Sprintf(`"Not found" redirects properly set for "%s"`, authority))
	return console.Success, nil
}

func answerPtr(answer any) *string {
	if s, ok := answer.(string); ok {
		return &s
	}
	return nil
}

func domainVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	input, err := visitsListInput(io.Input)
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	params, err := input.params()
	if err != nil {
		return 0, err
	}
	paginator, err := s.Visits.VisitsForDomain(env.Ctx, deref(io.Input.ArgumentString("domain"), ""), params, nil)
	if err != nil {
		return 0, err
	}
	return console.Success, renderVisits(env, style, input, paginator)
}
