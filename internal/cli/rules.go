package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

func init() {
	register("short-url:manage-rules", Body{Execute: manageRedirectRules})
}

// Redirect rule handler actions, like RedirectRuleHandlerAction.
const (
	ruleActionAdd       = "Add new rule"
	ruleActionRemove    = "Remove existing rule"
	ruleActionReArrange = "Re-arrange rule"
	ruleActionSave      = "Save and exit"
	ruleActionDiscard   = "Discard changes"
)

var ruleActions = []string{ruleActionAdd, ruleActionRemove, ruleActionReArrange, ruleActionSave, ruleActionDiscard}

func manageRedirectRules(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	id := shortURLIdentifier(io.Input)
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	su, err := s.ShortURLs.ResolveShortURL(env.Ctx, id, nil)
	if err != nil {
		if core.IsKind(err, core.KindShortURLNotFound) {
			style.Error(fmt.Sprintf("Short URL for %s not found", id))
			return console.Failure, nil
		}
		return 0, err
	}
	rules, err := s.Store.RulesForShortURL(env.Ctx, su.ID)
	if err != nil {
		return 0, err
	}
	toSave, save, err := manageRules(style, rules)
	if err != nil {
		return 0, err
	}
	if save {
		if _, err := s.Store.SetRulesForShortURL(env.Ctx, su.ID, toSave); err != nil {
			return 0, err
		}
		style.Success("Rules properly saved")
	}
	return console.Success, nil
}

// manageRules ports RedirectRuleHandler::manageRules(). save is false when changes are discarded.
func manageRules(io *console.SymfonyStyle, rules []core.RedirectRule) ([]core.RedirectRule, bool, error) {
	for {
		if len(rules) == 0 {
			io.NewLine(1)
			io.Text("<comment>// No rules found.</comment>")
		} else {
			width := len(strconv.Itoa(len(rules)))
			listing := make([]any, len(rules))
			for i, r := range rules {
				conditions := make([]string, len(r.Conditions))
				for j, c := range r.Conditions {
					conditions[j] = fmt.Sprintf("<comment>%s</comment>", c.HumanFriendly())
				}
				priority := strconv.Itoa(i + 1)
				priority = strings.Repeat("0", max(0, width-len(priority))) + priority
				listing[i] = []any{priority, strings.Join(conditions, " AND "), r.LongURL}
			}
			io.Table([]any{"Priority", "Conditions", "Redirect to"}, listing)
		}
		action, err := io.Choice("What do you want to do next?", ruleActions, strPtr(ruleActionSave))
		if err != nil {
			return nil, false, err
		}
		switch action {
		case ruleActionAdd:
			if rules, err = addRule(io, rules); err != nil {
				return nil, false, err
			}
		case ruleActionRemove:
			if rules, err = removeRule(io, rules); err != nil {
				return nil, false, err
			}
		case ruleActionReArrange:
			if rules, err = reArrangeRule(io, rules); err != nil {
				return nil, false, err
			}
		case ruleActionSave:
			return rules, true, nil
		case ruleActionDiscard:
			return nil, false, nil
		default:
			return nil, false, fmt.Errorf("unknown action %q", action)
		}
	}
}

func addRule(io *console.SymfonyStyle, current []core.RedirectRule) ([]core.RedirectRule, error) {
	priority, err := askPriority(io, len(current)+1)
	if err != nil {
		return nil, err
	}
	longURL, err := askLongURL(io)
	if err != nil {
		return nil, err
	}
	types := make([]string, len(core.AllRedirectConditionTypes))
	for i, t := range core.AllRedirectConditionTypes {
		types[i] = string(t)
	}
	var conditions []core.RedirectCondition
	for {
		typ, err := io.Choice("Type of the condition?", types, nil)
		if err != nil {
			return nil, err
		}
		c, err := askCondition(io, core.RedirectConditionType(typ))
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, c)
		more, err := io.Confirm("Do you want to add another condition?", true)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	rule := core.RedirectRule{Priority: priority, LongURL: longURL, Conditions: conditions}
	out := append([]core.RedirectRule{}, current[:priority-1]...)
	out = append(out, rule)
	return append(out, current[priority-1:]...), nil
}

// askCondition asks the values of a condition of the given type, like the match in RedirectRuleHandler::addRule().
func askCondition(io *console.SymfonyStyle, typ core.RedirectConditionType) (core.RedirectCondition, error) {
	c := core.RedirectCondition{Type: typ}
	var err error
	value := func(v string, e error) { c.MatchValue, err = &v, e }
	key := func(v string, e error) { c.MatchKey, err = &v, e }
	switch typ {
	case core.CondDevice:
		value(io.Choice("Device to match?", core.DeviceTypeValues, nil))
	case core.CondLanguage:
		value(askMandatory(io, "Language to match?"))
	case core.CondQueryParam:
		if key(askMandatory(io, "Query param name?")); err == nil {
			value(askOptional(io, "Query param value?"))
		}
	case core.CondAnyValueQueryParam, core.CondValuelessQueryParam:
		key(askMandatory(io, "Query param name?"))
	case core.CondIPAddress:
		value(askMandatory(io, "IP address, CIDR block or wildcard-pattern (1.2.*.*)"))
	case core.CondGeolocationCountryCode:
		value(askMandatory(io, "Country code to match?"))
	case core.CondGeolocationCityName:
		value(askMandatory(io, "City name to match?"))
	case core.CondBeforeDate, core.CondAfterDate:
		var date string
		if date, err = askMandatory(io, "Date to match?"); err == nil {
			var t, perr = normalizeOptionalDate(&date)
			if perr != nil {
				return c, perr
			}
			value(phpx.AtomString(*t), nil)
		}
	case core.CondBrowser:
		value(io.Choice("Browser to match?", core.BrowserValues, nil))
	default:
		return c, fmt.Errorf("unknown condition type %q", typ)
	}
	return c, err
}

func removeRule(io *console.SymfonyStyle, current []core.RedirectRule) ([]core.RedirectRule, error) {
	if len(current) == 0 {
		io.Warning("There are no rules to remove")
		return current, nil
	}
	index, err := askRule(io, "What rule do you want to delete?", current)
	if err != nil {
		return nil, err
	}
	return append(append([]core.RedirectRule{}, current[:index]...), current[index+1:]...), nil
}

func reArrangeRule(io *console.SymfonyStyle, current []core.RedirectRule) ([]core.RedirectRule, error) {
	if len(current) == 0 {
		io.Warning("There are no rules to re-arrange")
		return current, nil
	}
	oldIndex, err := askRule(io, "What rule do you want to re-arrange?", current)
	if err != nil {
		return nil, err
	}
	priority, err := askPriority(io, len(current))
	if err != nil {
		return nil, err
	}
	newIndex := priority - 1
	rule := current[oldIndex]
	rest := append(append([]core.RedirectRule{}, current[:oldIndex]...), current[oldIndex+1:]...)
	out := append([]core.RedirectRule{}, rest[:newIndex]...)
	out = append(out, rule)
	return append(out, rest[newIndex:]...), nil
}

// askRule asks for a rule among choices keyed by their priority, and returns its index.
func askRule(io *console.SymfonyStyle, message string, current []core.RedirectRule) (int, error) {
	keys := make([]string, len(current))
	choices := make([]string, len(current))
	for i, r := range current {
		keys[i] = strconv.Itoa(i + 1)
		choices[i] = fmt.Sprintf("%d - %s", i+1, r.LongURL)
	}
	answer, err := io.AskQuestion(console.NewKeyedChoiceQuestion(message, keys, choices, nil))
	if err != nil {
		return 0, err
	}
	for i, c := range choices {
		if c == answer {
			return i, nil
		}
	}
	return 0, errors.New("unknown rule")
}

func askPriority(io *console.SymfonyStyle, maxPriority int) (int, error) {
	answer, err := io.Ask("Rule priority (the lower the value, the higher the priority)", strPtr(strconv.Itoa(maxPriority)),
		func(answer any) (any, error) {
			s, _ := answer.(string)
			if !phpNumeric.MatchString(s) {
				return nil, errors.New("The priority must be a numeric positive value")
			}
			f, _ := strconv.ParseFloat(strings.Trim(s, " \t\n\r\v\f"), 64)
			return max(1, min(maxPriority, int(f))), nil
		})
	if err != nil {
		return 0, err
	}
	p, _ := answer.(int)
	return p, nil
}

func askLongURL(io *console.SymfonyStyle) (string, error) {
	answer, err := io.Ask("Long URL to redirect when the rule matches", nil, func(answer any) (any, error) {
		s, ok := answer.(string)
		if !ok {
			return nil, errors.New("The long URL is mandatory")
		}
		if !core.IsLooseURI(s) {
			return nil, errors.New("Provided value does not match a loose URI pattern")
		}
		return s, nil
	})
	s, _ := answer.(string)
	return s, err
}

func askMandatory(io *console.SymfonyStyle, message string) (string, error) {
	answer, err := io.Ask(message, nil, func(answer any) (any, error) {
		s, ok := answer.(string)
		if !ok {
			return nil, errors.New("The value is mandatory")
		}
		return strings.Trim(s, " \t\n\r\x00\x0B"), nil
	})
	s, _ := answer.(string)
	return s, err
}

func askOptional(io *console.SymfonyStyle, message string) (string, error) {
	answer, err := io.Ask(message, nil, func(answer any) (any, error) {
		s, _ := answer.(string)
		return strings.Trim(s, " \t\n\r\x00\x0B"), nil
	})
	s, _ := answer.(string)
	return s, err
}
