package core

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// InternalError marks failures upstream lets escape as unhandled exceptions (rendered as a 500 problem details
// response by the REST API), such as unparseable dates or invalid slugs detected inside a model constructor.
type InternalError struct{ Cause error }

func (e *InternalError) Error() string { return e.Cause.Error() }
func (e *InternalError) Unwrap() error { return e.Cause }

func internal(err error) error { return &InternalError{Cause: err} }

// ShortURLCreation ports ShortUrl\Model\ShortUrlCreation.
type ShortURLCreation struct {
	LongURL                  string
	ValidSince               *time.Time
	ValidUntil               *time.Time
	ShortURLMode             config.ShortURLMode
	MultiSegmentSlugsEnabled bool
	CustomSlug               *string
	PathPrefix               *string
	MaxVisits                *int
	FindIfExists             bool
	Domain                   *string
	ShortCodeLength          int
	APIKey                   *APIKey
	Tags                     []string
	TagKeys                  []string
	Title                    *string
	TitleWasAutoResolved     bool
	Crawlable                bool
	ForwardQuery             bool
}

// CreationDefaults are values injected by the caller rather than read from user input (ShortUrlOptionsPayloadMiddleware,
// AuthenticationMiddleware and the CLI).
type CreationDefaults struct {
	ShortURLMode             config.ShortURLMode
	MultiSegmentSlugsEnabled bool
	DefaultShortCodeLength   int
	APIKey                   *APIKey
	// ForcedDomain overrides the payload's domain (OverrideDomainMiddleware for domain-restricted API keys).
	ForcedDomain *string
}

// mapper accumulates Valinor-like mapping errors over an input.
type mapper struct {
	in   valinor.Input
	errs valinor.Errors
	loc  *time.Location
	now  time.Time
	// fatal is a non-mapping error (upstream exception) that aborts mapping.
	fatal error
}

func newMapper(in valinor.Input, loc *time.Location, now time.Time) *mapper {
	if in == nil {
		in = valinor.Input{}
	}
	return &mapper{in: in, loc: loc, now: now}
}

func (m *mapper) result() error {
	if m.fatal != nil {
		return m.fatal
	}
	if !m.errs.Empty() {
		return mappingError(&m.errs)
	}
	return nil
}

// mappingError builds the validation error for recorded mapping errors, keeping each node's message.
func mappingError(errs *valinor.Errors) *ProblemError {
	e := ErrValidationWithNames(errs.Names())
	for i, p := range errs.Paths() {
		e.Mapping = append(e.Mapping, MappingNode{Path: p, Message: errs.Messages()[i]})
	}
	return e
}

// ErrValidationWithNames builds a validation error whose invalid elements may contain integers (list indexes).
func ErrValidationWithNames(names []any) *ProblemError {
	e := ErrValidation()
	e.Additional = []KV{{"invalidElements", names}}
	return e
}

// requiredString maps a required string param. convert is applied to valid values.
func (m *mapper) requiredString(name string, convert converter) string {
	v, ok := m.in[name]
	if !ok || v == nil {
		m.errs.Add(name, "Value *missing* is not a valid string.")
		return ""
	}
	s, valid := valinor.ToString(v)
	if !valid {
		m.errs.Add(name, "invalid string")
		return ""
	}
	if convert != nil {
		var msg string
		if s, msg = convert(s); msg != "" {
			m.errs.Add(name, msg)
		}
	}
	return s
}

// optionalString maps a `string|null` param. Missing and null give nil.
func (m *mapper) optionalString(name string, convert converter) *string {
	v, ok := m.in[name]
	if !ok || v == nil {
		return nil
	}
	s, valid := valinor.ToString(v)
	if !valid {
		m.errs.Add(name, "invalid string")
		return nil
	}
	if convert != nil {
		var msg string
		if s, msg = convert(s); msg != "" {
			m.errs.Add(name, msg)
			return nil
		}
	}
	return &s
}

// optionalInt maps an `int|null` param.
func (m *mapper) optionalInt(name string) *int {
	v, ok := m.in[name]
	if !ok || v == nil {
		return nil
	}
	i, valid := valinor.ToInt(v)
	if !valid {
		m.errs.Add(name, "invalid int")
		return nil
	}
	return &i
}

// intMin maps an `int<min, max>` param with a default.
func (m *mapper) intMin(name string, def, min int) int {
	v, ok := m.in[name]
	if !ok {
		return def
	}
	i, valid := valinor.ToInt(v)
	if !valid || i < min {
		msg := "invalid int"
		if valid && min == 1 {
			msg = fmt.Sprintf("Value %d is not a valid positive integer.", i)
		}
		m.errs.Add(name, msg)
		return def
	}
	return i
}

// boolParam maps a `bool` param with a default.
func (m *mapper) boolParam(name string, def bool) bool {
	v, ok := m.in[name]
	if !ok {
		return def
	}
	b, valid := valinor.ToBool(v)
	if !valid {
		m.errs.Add(name, "invalid bool")
		return def
	}
	return b
}

// optionalBool maps a `bool|null` param.
func (m *mapper) optionalBool(name string) *bool {
	v, ok := m.in[name]
	if !ok || v == nil {
		return nil
	}
	b, valid := valinor.ToBool(v)
	if !valid {
		m.errs.Add(name, "invalid bool")
		return nil
	}
	return &b
}

// date maps a `DateTimeInterface|string|null` param through normalizeOptionalDate(). Unparseable dates are upstream
// exceptions, not mapping errors.
func (m *mapper) date(name string) *time.Time {
	v, ok := m.in[name]
	if !ok || v == nil {
		return nil
	}
	t, err := m.parseDateValue(v)
	if err != nil {
		if errors.Is(err, phpx.ErrMalformedDate) {
			m.fatal = internal(err)
		} else {
			m.errs.Add(name, "invalid date")
		}
		return nil
	}
	return &t
}

func (m *mapper) parseDateValue(v any) (time.Time, error) {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return time.Unix(i, 0).In(m.loc), nil
		}
	}
	s, ok := valinor.ToString(v)
	if !ok {
		return time.Time{}, errors.New("not a date")
	}
	t, err := phpx.ParseDate(s, m.loc, m.now)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(m.loc), nil
}

// tags maps an `array<string>` param through TagsConverter. Keys are the PHP array keys TagsConverter keeps.
func (m *mapper) tags(name string, nullable bool) ([]string, []string, bool) {
	v, ok := m.in[name]
	if !ok {
		return nil, nil, false
	}
	if v == nil {
		if !nullable {
			m.errs.Add(name, "Cannot be empty")
		}
		return nil, nil, false
	}
	list, keys, valid := valinor.ToStringListKeyed(v, &m.errs)
	if list == nil && !valid {
		m.errs.Add(name, "invalid list")
		return nil, nil, false
	}
	if !valid {
		return nil, nil, false
	}
	tags, tagKeys := NormalizeTags(list, keys)
	return tags, tagKeys, true
}

// NormalizeTags ports TagsConverter: every tag lower-cased (multibyte aware), trimmed, stripped of tags and with
// spaces as dashes, then array_unique(), which keeps the first occurrence of each value with its key.
func NormalizeTags(tags, keys []string) ([]string, []string) {
	out := make([]string, 0, len(tags))
	var outKeys []string
	if keys != nil {
		outKeys = make([]string, 0, len(tags))
	}
	seen := map[string]bool{}
	for i, t := range tags {
		n := strings.ReplaceAll(strings.ToLower(valinor.PHPTrim(valinor.StripTags(t))), " ", "-")
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if keys != nil {
			outKeys = append(outKeys, keys[i])
		}
	}
	return out, outKeys
}

// looseURI ports LooseUriConverter: anything with a "scheme:rest" shape.
func looseURI(s string) (string, bool) {
	i := strings.Index(s[min(1, len(s)):], ":")
	if i < 0 {
		return s, false
	}
	// pattern /(.+):(.+)/ – at least one char on each side of some colon
	for j := 1; j < len(s)-1; j++ {
		if s[j] == ':' {
			return s, true
		}
	}
	return s, false
}

// converter ports a Valinor converter for strings: it returns the converted value, or the message of the mapping
// error it throws.
type converter func(string) (string, string)

// IsLooseURI tells if a value passes LooseUriConverter.
func IsLooseURI(s string) bool {
	_, ok := looseURI(s)
	return ok
}

// looseURIConverter ports LooseUriConverter.
func looseURIConverter(s string) (string, string) {
	if _, ok := looseURI(s); !ok {
		return s, "Provided value does not match a loose URI pattern"
	}
	return s, ""
}

// substring512 ports SubstringConverter(512): trim, then cut to 512 bytes.
func substring512(s string) (string, string) {
	s = valinor.PHPTrim(s)
	if len(s) > 512 {
		s = s[:512]
	}
	return s, ""
}

func trimStrip(s string) (string, string) { return valinor.TrimStrip(s), "" }

// MapShortURLCreation maps a payload to ShortURLCreation like upstream's TreeMapper does.
func MapShortURLCreation(in valinor.Input, d CreationDefaults, loc *time.Location, now time.Time) (*ShortURLCreation, error) {
	m := newMapper(in, loc, now)
	c := &ShortURLCreation{
		ShortURLMode:             d.ShortURLMode,
		MultiSegmentSlugsEnabled: d.MultiSegmentSlugsEnabled,
		APIKey:                   d.APIKey,
		ForwardQuery:             true,
	}
	if c.ShortURLMode == "" {
		c.ShortURLMode = config.ModeStrict
	}
	c.LongURL = m.requiredString("longUrl", looseURIConverter)
	c.ValidSince = m.date("validSince")
	c.ValidUntil = m.date("validUntil")
	if m.fatal != nil {
		return nil, m.fatal
	}
	customSlug := m.optionalString("customSlug", trimStrip)
	pathPrefix := m.optionalString("pathPrefix", trimStrip)
	c.MaxVisits = m.optionalInt("maxVisits")
	c.FindIfExists = m.boolParam("findIfExists", false)
	if d.ForcedDomain != nil {
		c.Domain = d.ForcedDomain
	} else {
		c.Domain = m.optionalString("domain", hostAndPort)
	}
	defLength := d.DefaultShortCodeLength
	if defLength == 0 {
		defLength = config.DefaultShortCodesLengthValue
	}
	if _, provided := in["shortCodeLength"]; provided && in["shortCodeLength"] != nil {
		c.ShortCodeLength = m.intMin("shortCodeLength", defLength, config.MinShortCodesLength)
	} else if provided && in["shortCodeLength"] == nil {
		// ShortUrlOptionsPayloadMiddleware uses isset(): null is replaced by the default
		c.ShortCodeLength = defLength
	} else {
		c.ShortCodeLength = defLength
	}
	if tags, keys, ok := m.tags("tags", false); ok {
		c.Tags, c.TagKeys = tags, keys
	}
	c.Title = m.optionalString("title", substring512)
	c.TitleWasAutoResolved = m.boolParam("titleWasAutoResolved", false)
	c.Crawlable = m.boolParam("crawlable", false)
	c.ForwardQuery = m.boolParam("forwardQuery", true)
	if err := m.result(); err != nil {
		return nil, err
	}
	var err error
	if c.CustomSlug, err = filterSlugLike(customSlug, c.ShortURLMode, c.MultiSegmentSlugsEnabled); err != nil {
		return nil, err
	}
	if c.PathPrefix, err = filterSlugLike(pathPrefix, c.ShortURLMode, c.MultiSegmentSlugsEnabled); err != nil {
		return nil, err
	}
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

// filterSlugLike ports ShortUrlCreation::filterAndValidateOptionsRelatedValue(). Reserved characters make upstream's
// constructor throw, which surfaces as a server error.
func filterSlugLike(v *string, mode config.ShortURLMode, multiSegment bool) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := *v
	if mode == config.ModeLoose {
		s = asciiLower(s)
	}
	reserved := ":?#[]@"
	if multiSegment {
		s = strings.Trim(strings.ReplaceAll(s, " ", "-"), "/")
	} else {
		s = strings.NewReplacer(" ", "-", "/", "-").Replace(s)
		reserved += "/"
	}
	if strings.ContainsAny(s, reserved) {
		return nil, internal(errors.New("URL-reserved characters cannot be used in a custom slug or path prefix"))
	}
	return &s, nil
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// HasTitle implements the title resolution model.
func (c *ShortURLCreation) HasTitle() bool { return c.Title != nil }

// GenerateRandomShortCode ports generateRandomShortCode() (nanoid with Shlink's alphabets).
func GenerateRandomShortCode(length int, mode config.ShortURLMode) string {
	alphabet := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if mode == config.ModeLoose {
		alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	}
	return nanoid(alphabet, length)
}

func nanoid(alphabet string, size int) string {
	mask := (2 << (bits.Len(uint(len(alphabet)-1)) - 1)) - 1
	step := (8*mask*size)/(5*len(alphabet)) + 1
	id := make([]byte, 0, size)
	buf := make([]byte, step)
	for {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		for _, b := range buf {
			idx := int(b) & mask
			if idx < len(alphabet) {
				id = append(id, alphabet[idx])
				if len(id) == size {
					return string(id)
				}
			}
		}
	}
}
