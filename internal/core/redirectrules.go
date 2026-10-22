package core

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/useragent"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// RedirectConditionType ports RedirectRule\Model\RedirectConditionType.
type RedirectConditionType string

const (
	CondDevice                 RedirectConditionType = "device"
	CondLanguage               RedirectConditionType = "language"
	CondQueryParam             RedirectConditionType = "query-param"
	CondAnyValueQueryParam     RedirectConditionType = "any-value-query-param"
	CondValuelessQueryParam    RedirectConditionType = "valueless-query-param"
	CondIPAddress              RedirectConditionType = "ip-address"
	CondGeolocationCountryCode RedirectConditionType = "geolocation-country-code"
	CondGeolocationCityName    RedirectConditionType = "geolocation-city-name"
	CondBeforeDate             RedirectConditionType = "before-date"
	CondAfterDate              RedirectConditionType = "after-date"
	CondBrowser                RedirectConditionType = "browser"
)

// AllRedirectConditionTypes lists the types in upstream's order.
var AllRedirectConditionTypes = []RedirectConditionType{
	CondDevice, CondLanguage, CondQueryParam, CondAnyValueQueryParam, CondValuelessQueryParam, CondIPAddress,
	CondGeolocationCountryCode, CondGeolocationCityName, CondBeforeDate, CondAfterDate, CondBrowser,
}

// enumName returns the PHP enum case name (DEVICE, QUERY_PARAM, ...).
func (t RedirectConditionType) enumName() string {
	return strings.ToUpper(strings.ReplaceAll(string(t), "-", "_"))
}

// DeviceTypeValues and BrowserValues are the values of the DeviceType and Browser enums.
var (
	DeviceTypeValues = []string{"android", "ios", "mobile", "windows", "macos", "linux", "chromeos", "desktop"}
	BrowserValues    = []string{"chrome", "firefox", "edge", "safari", "opera", "android_browser"}
)

// ISOCountryCodes is the ISO_COUNTRY_CODES constant.
var ISOCountryCodes = []string{"AF", "AX", "AL", "DZ", "AS", "AD", "AO", "AI", "AQ", "AG", "AR", "AM", "AW", "AU", "AT", "AZ", "BS", "BH", "BD", "BB", "BY", "BE", "BZ", "BJ", "BM", "BT", "BO", "BQ", "BA", "BW", "BV", "BR", "IO", "BN", "BG", "BF", "BI", "CV", "KH", "CM", "CA", "KY", "CF", "TD", "CL", "CN", "CX", "CC", "CO", "KM", "CG", "CD", "CK", "CR", "CI", "HR", "CU", "CW", "CY", "CZ", "DK", "DJ", "DM", "DO", "EC", "EG", "SV", "GQ", "ER", "EE", "SZ", "ET", "FK", "FO", "FJ", "FI", "FR", "GF", "PF", "TF", "GA", "GM", "GE", "DE", "GH", "GI", "GR", "GL", "GD", "GP", "GU", "GT", "GG", "GN", "GW", "GY", "HT", "HM", "VA", "HN", "HK", "HU", "IS", "IN", "ID", "IR", "IQ", "IE", "IM", "IL", "IT", "JM", "JP", "JE", "JO", "KZ", "KE", "KI", "KP", "KR", "KW", "KG", "LA", "LV", "LB", "LS", "LR", "LY", "LI", "LT", "LU", "MO", "MG", "MW", "MY", "MV", "ML", "MT", "MH", "MQ", "MR", "MU", "YT", "MX", "FM", "MD", "MC", "MN", "ME", "MS", "MA", "MZ", "MM", "NA", "NR", "NP", "NL", "NC", "NZ", "NI", "NE", "NG", "NU", "NF", "MK", "MP", "NO", "OM", "PK", "PW", "PS", "PA", "PG", "PY", "PE", "PH", "PN", "PL", "PT", "PR", "QA", "RE", "RO", "RU", "RW", "BL", "SH", "KN", "LC", "MF", "PM", "VC", "WS", "SM", "ST", "SA", "SN", "RS", "SC", "SL", "SG", "SX", "SK", "SI", "SB", "SO", "ZA", "GS", "SS", "ES", "LK", "SD", "SR", "SJ", "SE", "CH", "SY", "TW", "TJ", "TZ", "TH", "TL", "TG", "TK", "TO", "TT", "TN", "TR", "TM", "TC", "TV", "UG", "UA", "AE", "GB", "US", "UM", "UY", "UZ", "VU", "VE", "VN", "VG", "VI", "WF", "EH", "YE", "ZM", "ZW"}

// IsValidConditionValue ports RedirectConditionType::isValid().
func IsValidConditionValue(t RedirectConditionType, value string) bool {
	switch t {
	case CondDevice:
		return contains(DeviceTypeValues, value)
	case CondBrowser:
		return contains(BrowserValues, value)
	case CondIPAddress:
		return IsStaticIPCidrOrWildcard(value)
	case CondGeolocationCountryCode:
		return contains(ISOCountryCodes, value)
	case CondQueryParam:
		return value != ""
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// RedirectCondition ports the RedirectCondition entity.
type RedirectCondition struct {
	ID         int64
	Type       RedirectConditionType
	MatchKey   *string
	MatchValue *string
}

// EncodePHP serializes like RedirectCondition::jsonSerialize().
func (c RedirectCondition) EncodePHP(int) any {
	return phpx.Map{{Key: "type", Value: string(c.Type)}, {Key: "matchKey", Value: c.MatchKey}, {Key: "matchValue", Value: c.MatchValue}}
}

// HumanFriendly ports RedirectCondition::toHumanFriendly(), including upstream's use of matchValue for the
// any-value and valueless query param conditions.
func (c RedirectCondition) HumanFriendly() string {
	v := deref(c.MatchValue)
	switch c.Type {
	case CondDevice:
		return "device is " + v
	case CondLanguage:
		return v + " language is accepted"
	case CondQueryParam:
		return fmt.Sprintf("query string contains %s=%s", deref(c.MatchKey), v)
	case CondAnyValueQueryParam:
		return fmt.Sprintf("query string contains %s param", v)
	case CondValuelessQueryParam:
		return fmt.Sprintf("query string contains %s param without a value (https://example.com?foo)", v)
	case CondIPAddress:
		return "IP address matches " + v
	case CondGeolocationCountryCode:
		return "country code is " + v
	case CondGeolocationCityName:
		return "city name is " + v
	case CondBeforeDate:
		return "date is before " + v
	case CondAfterDate:
		return "date is after " + v
	case CondBrowser:
		return "browser is " + v
	}
	return ""
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// RedirectRule ports the ShortUrlRedirectRule entity.
type RedirectRule struct {
	ID         int64
	Priority   int
	LongURL    string
	Conditions []RedirectCondition
}

// EncodePHP serializes like ShortUrlRedirectRule::jsonSerialize().
func (r RedirectRule) EncodePHP(int) any {
	conds := make([]any, len(r.Conditions))
	for i, c := range r.Conditions {
		conds[i] = c
	}
	return phpx.Map{{Key: "longUrl", Value: r.LongURL}, {Key: "priority", Value: r.Priority}, {Key: "conditions", Value: conds}}
}

// RuleRequest is the request information redirect conditions are evaluated against.
type RuleRequest struct {
	// Query is the request's query params parsed the PHP way (getQueryParams()).
	Query          map[string]any
	AcceptLanguage string
	UserAgent      string
	RemoteAddress  *string
	Geolocation    *Location
	Now            time.Time
}

// Matches ports RedirectCondition::matchesRequest().
func (c RedirectCondition) Matches(r *RuleRequest) bool {
	switch c.Type {
	case CondQueryParam:
		if c.MatchKey == nil {
			return false
		}
		v, ok := r.Query[*c.MatchKey].(string)
		return ok && c.MatchValue != nil && v == *c.MatchValue
	case CondValuelessQueryParam:
		if c.MatchKey == nil {
			return false
		}
		v, ok := r.Query[*c.MatchKey]
		return ok && phpEmpty(v)
	case CondAnyValueQueryParam:
		if c.MatchKey == nil {
			return false
		}
		_, ok := r.Query[*c.MatchKey]
		return ok
	case CondLanguage:
		return c.matchesLanguage(r.AcceptLanguage)
	case CondDevice:
		for _, d := range useragent.DeviceTypesFromUserAgent(r.UserAgent) {
			if c.MatchValue != nil && string(d) == *c.MatchValue {
				return true
			}
		}
		return false
	case CondIPAddress:
		if c.MatchValue == nil || r.RemoteAddress == nil {
			return false
		}
		m, _ := IPAddressMatchesGroups(*r.RemoteAddress, []string{*c.MatchValue})
		return m
	case CondGeolocationCountryCode:
		return r.Geolocation != nil && c.MatchValue != nil && strings.EqualFold(r.Geolocation.CountryCode, *c.MatchValue)
	case CondGeolocationCityName:
		return r.Geolocation != nil && c.MatchValue != nil && strings.EqualFold(r.Geolocation.City, *c.MatchValue)
	case CondBeforeDate, CondAfterDate:
		t, err := phpx.ParseDate(deref(c.MatchValue), r.Now.Location(), r.Now)
		if err != nil {
			return false
		}
		if c.Type == CondBeforeDate {
			return r.Now.Before(t)
		}
		return r.Now.After(t)
	case CondBrowser:
		b, ok := useragent.BrowserFromUserAgent(r.UserAgent)
		return ok && c.MatchValue != nil && string(b) == *c.MatchValue
	}
	return false
}

// phpEmpty ports PHP's empty() for query values: "", "0", empty arrays.
func phpEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == "" || t == "0"
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

func (c RedirectCondition) matchesLanguage(acceptLanguage string) bool {
	acceptLanguage = strings.TrimSpace(acceptLanguage)
	if acceptLanguage == "" || acceptLanguage == "*" || c.MatchValue == nil {
		return false
	}
	matchLang, matchCountry := splitLocale(normalizeLocale(*c.MatchValue))
	for _, lang := range acceptLanguageToLocales(acceptLanguage, 0.9) {
		language, country := splitLocale(lang)
		if matchLang != language {
			continue
		}
		if matchCountry == nil || (country != nil && *matchCountry == *country) {
			return true
		}
	}
	return false
}

func normalizeLocale(l string) string {
	return strings.TrimSpace(strings.ToLower(strings.ReplaceAll(l, "_", "-")))
}

func splitLocale(l string) (string, *string) {
	lang, country, ok := strings.Cut(l, "-")
	if !ok {
		return lang, nil
	}
	// explode() then array_pad to 2: only the second segment is the country code
	if i := strings.Index(country, "-"); i >= 0 {
		country = country[:i]
	}
	return lang, &country
}

// acceptLanguageToLocales ports acceptLanguageToLocales().
func acceptLanguageToLocales(acceptLanguage string, minQuality float64) []string {
	var out []string
	for _, item := range strings.Split(acceptLanguage, ",") {
		lang, qualityString, _ := strings.Cut(item, ";")
		normalized := normalizeLocale(lang)
		quality := 1.0
		q := phpx.ParseQuery(strings.TrimSpace(qualityString))
		if v, ok := q.Get("q"); ok && v != nil {
			quality = phpFloatCast(*v)
		}
		if normalized != "*" && quality >= minQuality {
			out = append(out, normalized)
		}
	}
	return out
}

func phpFloatCast(s string) float64 {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &f)
	if err != nil {
		return 0
	}
	return f
}

// RulesForShortURL ports ShortUrlRedirectRuleService::rulesForShortUrl(): rules by priority, conditions by id.
func (s *Store) RulesForShortURL(ctx context.Context, shortURLID int64) ([]RedirectRule, error) {
	rows, err := s.DB.Query(ctx, s.DB.DB, `SELECT r.id, r.priority, r.long_url, c.id, c.type, c.match_key, c.match_value
		FROM short_url_redirect_rules r
		LEFT JOIN redirect_conditions_in_short_url_redirect_rules rc ON rc.short_url_redirect_rule_id = r.id
		LEFT JOIN redirect_conditions c ON c.id = rc.redirect_condition_id
		WHERE r.short_url_id = ? ORDER BY r.priority ASC, r.id ASC, c.id ASC`, shortURLID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []RedirectRule
	for rows.Next() {
		var (
			ruleID, priority int64
			longURL          string
			condID           sql.NullInt64
			typ, key, value  sql.NullString
		)
		if err := rows.Scan(&ruleID, &priority, &longURL, &condID, &typ, &key, &value); err != nil {
			return nil, err
		}
		if len(rules) == 0 || rules[len(rules)-1].ID != ruleID {
			rules = append(rules, RedirectRule{ID: ruleID, Priority: int(priority), LongURL: longURL, Conditions: []RedirectCondition{}})
		}
		if condID.Valid {
			r := &rules[len(rules)-1]
			r.Conditions = append(r.Conditions, RedirectCondition{
				ID: condID.Int64, Type: RedirectConditionType(typ.String), MatchKey: strPtr(key), MatchValue: strPtr(value),
			})
		}
	}
	return rules, rows.Err()
}

// ResolveLongURL ports ShortUrlRedirectionResolver::resolveLongUrl().
func (s *Store) ResolveLongURL(ctx context.Context, su *ShortURL, r *RuleRequest) (string, error) {
	rules, err := s.RulesForShortURL(ctx, su.ID)
	if err != nil {
		return "", err
	}
	for _, rule := range rules {
		if len(rule.Conditions) == 0 {
			continue
		}
		all := true
		for _, c := range rule.Conditions {
			if !c.Matches(r) {
				all = false
				break
			}
		}
		if all {
			return rule.LongURL, nil
		}
	}
	return su.LongURL, nil
}

// SetRulesForShortURL ports setRulesForShortUrl()/saveRulesForShortUrl(): old rules and conditions are removed and the
// new ones get sequential priorities.
func (s *Store) SetRulesForShortURL(ctx context.Context, shortURLID int64, rules []RedirectRule) ([]RedirectRule, error) {
	err := s.DB.InTx(ctx, func(tx *sql.Tx) error { return s.setRules(ctx, tx, shortURLID, rules) })
	return rules, err
}

// setRules replaces the rules of a short URL inside the caller's transaction.
func (s *Store) setRules(ctx context.Context, tx *sql.Tx, shortURLID int64, rules []RedirectRule) error {
	oldConds, err := s.DB.Query(ctx, tx, `SELECT rc.redirect_condition_id FROM redirect_conditions_in_short_url_redirect_rules rc
		INNER JOIN short_url_redirect_rules r ON r.id = rc.short_url_redirect_rule_id WHERE r.short_url_id = ?`, shortURLID)
	if err != nil {
		return err
	}
	var condIDs []any
	for oldConds.Next() {
		var id int64
		if err := oldConds.Scan(&id); err != nil {
			oldConds.Close()
			return err
		}
		condIDs = append(condIDs, id)
	}
	oldConds.Close()
	if _, err := s.DB.Exec(ctx, tx, `DELETE FROM redirect_conditions_in_short_url_redirect_rules WHERE short_url_redirect_rule_id IN
		(SELECT id FROM short_url_redirect_rules WHERE short_url_id = ?)`, shortURLID); err != nil {
		return err
	}
	if len(condIDs) > 0 {
		if _, err := s.DB.Exec(ctx, tx, "DELETE FROM redirect_conditions WHERE id IN ("+placeholders(len(condIDs))+")", condIDs...); err != nil {
			return err
		}
	}
	if _, err := s.DB.Exec(ctx, tx, "DELETE FROM short_url_redirect_rules WHERE short_url_id = ?", shortURLID); err != nil {
		return err
	}
	for i := range rules {
		r := &rules[i]
		r.Priority = i + 1
		id, err := s.DB.Insert(ctx, tx, "short_url_redirect_rules", []string{"priority", "long_url", "short_url_id"}, r.Priority, r.LongURL, shortURLID)
		if err != nil {
			return err
		}
		r.ID = id
		for j := range r.Conditions {
			c := &r.Conditions[j]
			cid, err := s.DB.Insert(ctx, tx, "redirect_conditions", []string{"type", "match_key", "match_value"}, string(c.Type), nullable(c.MatchKey), nullable(c.MatchValue))
			if err != nil {
				return err
			}
			c.ID = cid
			if _, err := s.DB.Exec(ctx, tx, "INSERT INTO redirect_conditions_in_short_url_redirect_rules (short_url_redirect_rule_id, redirect_condition_id) VALUES (?, ?)", id, cid); err != nil {
				return err
			}
		}
	}
	return nil
}

// MapRedirectRulesData maps a payload like upstream's TreeMapper maps RedirectRulesData.
func MapRedirectRulesData(in valinor.Input) ([]RedirectRule, error) {
	var errs valinor.Errors
	raw, ok := in["redirectRules"]
	if !ok {
		return []RedirectRule{}, nil
	}
	list, isList := asList(raw)
	if !isList {
		errs.Add("redirectRules", "invalid")
		return nil, ErrValidationWithNames(errs.Names())
	}
	rules := make([]RedirectRule, 0, len(list))
	for ri, item := range list {
		obj, isObj := item.(map[string]any)
		if !isObj {
			errs.Add(ri, "invalid rule")
			continue
		}
		var rule RedirectRule
		lu, has := obj["longUrl"]
		s, valid := valinor.ToString(lu)
		if !has || lu == nil || !valid {
			errs.Add("longUrl", "invalid")
		} else if _, ok := looseURI(s); !ok {
			errs.Add("longUrl", "invalid")
		} else {
			rule.LongURL = s
		}
		condsRaw, hasConds := obj["conditions"]
		conds, condsOK := asList(condsRaw)
		if !hasConds || condsRaw == nil || !condsOK {
			errs.Add("conditions", "invalid")
			continue
		}
		for ci, ci2 := range conds {
			co, isObj := ci2.(map[string]any)
			if !isObj {
				errs.Add(ci, "invalid condition")
				continue
			}
			var c RedirectCondition
			t, _ := valinor.ToString(co["type"])
			if !contains(conditionTypeStrings(), t) {
				errs.Add("type", "invalid type")
				continue
			}
			c.Type = RedirectConditionType(t)
			fieldOK := true
			for _, f := range []struct {
				name string
				dst  **string
			}{{"matchKey", &c.MatchKey}, {"matchValue", &c.MatchValue}} {
				v, present := co[f.name]
				if !present || v == nil {
					continue
				}
				sv, ok := valinor.ToString(v)
				if !ok {
					errs.Add(f.name, "invalid")
					fieldOK = false
					continue
				}
				sv = valinor.TrimStrip(sv)
				*f.dst = &sv
			}
			if !fieldOK {
				continue
			}
			if c.MatchValue != nil && !IsValidConditionValue(c.Type, *c.MatchValue) {
				errs.Add(ci, fmt.Sprintf(`"%s" is not a valid value for the "%s" redirect condition`, *c.MatchValue, c.Type.enumName()))
				continue
			}
			rule.Conditions = append(rule.Conditions, c)
		}
		rules = append(rules, rule)
	}
	if !errs.Empty() {
		return nil, ErrValidationWithNames(errs.Names())
	}
	return rules, nil
}

func conditionTypeStrings() []string {
	out := make([]string, len(AllRedirectConditionTypes))
	for i, t := range AllRedirectConditionTypes {
		out[i] = string(t)
	}
	return out
}

// asList accepts JSON arrays and objects (PHP arrays), returning values in order.
func asList(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sortStrings(keys)
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = t[k]
		}
		return out, true
	}
	return nil, false
}
