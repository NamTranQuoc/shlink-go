// Package core ports Shlink's Core and Rest domain: entities, repositories and services for short URLs, visits, tags,
// domains, redirect rules and API keys.
package core

import (
	"errors"
	"fmt"
	"net/http"
)

// KV is an ordered key/value pair used for problem details' additional members.
type KV struct {
	Key   string
	Value any
}

// ProblemError is an error that maps to an RFC 7807 problem details response, like upstream's exceptions implementing
// ProblemDetailsExceptionInterface.
type ProblemError struct {
	Type       string
	Title      string
	Detail     string
	Status     int
	Additional []KV
	// Kind identifies the upstream exception class, for callers that need to tell errors apart.
	Kind string
	// Mapping holds the Valinor errors behind a validation error raised while mapping input, for callers (the CLI)
	// that surface the mapper's own exception instead.
	Mapping []MappingNode
}

// MappingNode is one Valinor mapping error: the path of the node and its message.
type MappingNode struct {
	Path    string
	Message string
}

func (e *ProblemError) Error() string { return e.Detail }

// ProblemType ports toProblemDetailsType().
func ProblemType(code string) string { return "https://shlink.io/api/error/" + code }

// Exception kinds.
const (
	KindDeleteShortURL       = "DeleteShortUrlException"
	KindDomainNotFound       = "DomainNotFoundException"
	KindForbiddenTagOp       = "ForbiddenTagOperationException"
	KindMalformedBody        = "MalformedBodyException"
	KindNonUniqueSlug        = "NonUniqueSlugException"
	KindShortURLNotFound     = "ShortUrlNotFoundException"
	KindTagConflict          = "TagConflictException"
	KindTagNotFound          = "TagNotFoundException"
	KindValidation           = "ValidationException"
	KindMercureNotConfigured = "MercureException"
	KindMissingAuth          = "MissingAuthenticationException"
	KindInvalidAPIKey        = "VerifyAuthenticationException"
)

// IsKind tells if err is a ProblemError of the given kind.
func IsKind(err error, kind string) bool {
	var pe *ProblemError
	return errors.As(err, &pe) && pe.Kind == kind
}

func identifierSuffix(domain *string) string {
	if domain == nil {
		return ""
	}
	return fmt.Sprintf(` for domain "%s"`, *domain)
}

// ErrShortURLNotFound ports ShortUrlNotFoundException::fromNotFound().
func ErrShortURLNotFound(id ShortURLIdentifier) *ProblemError {
	e := &ProblemError{
		Kind:       KindShortURLNotFound,
		Detail:     fmt.Sprintf(`No URL found with short code "%s"%s`, id.ShortCode, identifierSuffix(id.Domain)),
		Title:      "Short URL not found",
		Type:       ProblemType("short-url-not-found"),
		Status:     http.StatusNotFound,
		Additional: []KV{{"shortCode", id.ShortCode}},
	}
	if id.Domain != nil {
		e.Additional = append(e.Additional, KV{"domain", *id.Domain})
	}
	return e
}

// ErrDeleteShortURLThreshold ports DeleteShortUrlException::fromVisitsThreshold().
func ErrDeleteShortURLThreshold(threshold int, id ShortURLIdentifier) *ProblemError {
	e := &ProblemError{
		Kind: KindDeleteShortURL,
		Detail: fmt.Sprintf(`Impossible to delete short URL with short code "%s"%s, since it has more than "%d" visits.`,
			id.ShortCode, identifierSuffix(id.Domain), threshold),
		Title:      "Cannot delete short URL",
		Type:       ProblemType("invalid-short-url-deletion"),
		Status:     http.StatusUnprocessableEntity,
		Additional: []KV{{"shortCode", id.ShortCode}, {"threshold", threshold}},
	}
	if id.Domain != nil {
		e.Additional = append(e.Additional, KV{"domain", *id.Domain})
	}
	return e
}

// ErrNonUniqueSlug ports NonUniqueSlugException::fromSlug().
func ErrNonUniqueSlug(slug string, domain *string) *ProblemError {
	e := &ProblemError{
		Kind:       KindNonUniqueSlug,
		Detail:     fmt.Sprintf(`Provided slug "%s" is already in use%s.`, slug, identifierSuffix(domain)),
		Title:      "Invalid custom slug",
		Type:       ProblemType("non-unique-slug"),
		Status:     http.StatusBadRequest,
		Additional: []KV{{"customSlug", slug}},
	}
	if domain != nil {
		e.Additional = append(e.Additional, KV{"domain", *domain})
	}
	return e
}

// ErrDomainNotFoundByAuthority ports DomainNotFoundException::fromAuthority().
func ErrDomainNotFoundByAuthority(authority string) *ProblemError {
	return &ProblemError{
		Kind:       KindDomainNotFound,
		Detail:     fmt.Sprintf(`Domain with authority "%s" could not be found`, authority),
		Title:      "Domain not found",
		Type:       ProblemType("domain-not-found"),
		Status:     http.StatusNotFound,
		Additional: []KV{{"authority", authority}},
	}
}

// ErrDomainNotFoundByID ports DomainNotFoundException::fromId().
func ErrDomainNotFoundByID(id string) *ProblemError {
	return &ProblemError{
		Kind:       KindDomainNotFound,
		Detail:     fmt.Sprintf(`Domain with id "%s" could not be found`, id),
		Title:      "Domain not found",
		Type:       ProblemType("domain-not-found"),
		Status:     http.StatusNotFound,
		Additional: []KV{{"id", id}},
	}
}

func forbiddenTagOperation(message string) *ProblemError {
	return &ProblemError{
		Kind:   KindForbiddenTagOp,
		Detail: message,
		Title:  "Forbidden tag operation",
		Type:   ProblemType("forbidden-tag-operation"),
		Status: http.StatusForbidden,
	}
}

// ErrForbiddenTagDeletion and ErrForbiddenTagRenaming port ForbiddenTagOperationException.
func ErrForbiddenTagDeletion() *ProblemError {
	return forbiddenTagOperation("You are not allowed to delete tags")
}
func ErrForbiddenTagRenaming() *ProblemError {
	return forbiddenTagOperation("You are not allowed to rename tags")
}

// ErrMalformedBody ports MalformedBodyException::forInvalidJson().
func ErrMalformedBody() *ProblemError {
	return &ProblemError{
		Kind:   KindMalformedBody,
		Detail: "Provided request does not contain a valid JSON body.",
		Title:  "Malformed request body",
		Type:   ProblemType("malformed-request-body"),
		Status: http.StatusBadRequest,
	}
}

// ErrTagConflict ports TagConflictException::forExistingTag().
func ErrTagConflict(r Renaming) *ProblemError {
	return &ProblemError{
		Kind:       KindTagConflict,
		Detail:     fmt.Sprintf("You cannot rename tag %s, because it already exists", r.String()),
		Title:      "Tag conflict",
		Type:       ProblemType("tag-conflict"),
		Status:     http.StatusConflict,
		Additional: []KV{{"oldName", r.OldName}, {"newName", r.NewName}},
	}
}

// ErrTagNotFound ports TagNotFoundException::fromTag().
func ErrTagNotFound(tag string) *ProblemError {
	return &ProblemError{
		Kind:       KindTagNotFound,
		Detail:     fmt.Sprintf(`Tag with name "%s" could not be found`, tag),
		Title:      "Tag not found",
		Type:       ProblemType("tag-not-found"),
		Status:     http.StatusNotFound,
		Additional: []KV{{"tag", tag}},
	}
}

// ErrValidation ports ValidationException::fromArray(): only the names of the invalid elements are exposed.
func ErrValidation(invalidElements ...string) *ProblemError {
	if invalidElements == nil {
		invalidElements = []string{}
	}
	return &ProblemError{
		Kind:       KindValidation,
		Detail:     "Provided data is not valid",
		Title:      "Invalid data",
		Type:       ProblemType("invalid-data"),
		Status:     http.StatusBadRequest,
		Additional: []KV{{"invalidElements", invalidElements}},
	}
}

// ErrMercureNotConfigured ports MercureException::mercureNotConfigured().
func ErrMercureNotConfigured() *ProblemError {
	return &ProblemError{
		Kind:   KindMercureNotConfigured,
		Detail: "This Shlink instance is not integrated with a mercure hub.",
		Title:  "Mercure integration not configured",
		Type:   ProblemType("mercure-not-configured"),
		Status: http.StatusNotImplemented,
	}
}

// ErrMissingAuthentication ports MissingAuthenticationException::forHeaders().
func ErrMissingAuthentication(expectedHeaders []string) *ProblemError {
	quoted := ""
	for i, h := range expectedHeaders {
		if i > 0 {
			quoted += `", "`
		}
		quoted += h
	}
	return &ProblemError{
		Kind:       KindMissingAuth,
		Detail:     fmt.Sprintf(`Expected one of the following authentication headers, ["%s"], but none were provided`, quoted),
		Title:      "Invalid authorization",
		Type:       ProblemType("missing-authentication"),
		Status:     http.StatusUnauthorized,
		Additional: []KV{{"expectedHeaders", expectedHeaders}},
	}
}

// ErrInvalidAPIKey ports VerifyAuthenticationException::forInvalidApiKey().
func ErrInvalidAPIKey() *ProblemError {
	return &ProblemError{
		Kind:   KindInvalidAPIKey,
		Detail: "Provided API key does not exist or is invalid.",
		Title:  "Invalid API key",
		Type:   ProblemType("invalid-api-key"),
		Status: http.StatusUnauthorized,
	}
}

// Plain errors (not exposed as problem details).
var (
	ErrAPIKeyNotFound = errors.New("api key not found")
)

// APIKeyNotFoundError ports ApiKeyNotFoundException::forName().
type APIKeyNotFoundError struct{ Name string }

func (e APIKeyNotFoundError) Error() string {
	return fmt.Sprintf(`API key with name "%s" not found`, e.Name)
}

// APIKeyConflictError ports ApiKeyConflictException::forName().
type APIKeyConflictError struct{ Name string }

func (e APIKeyConflictError) Error() string {
	return fmt.Sprintf(`An API key with name "%s" already exists`, e.Name)
}

// InvalidArgumentError ports shlink-common's InvalidArgumentException used by services with a plain message.
type InvalidArgumentError struct{ Message string }

func (e InvalidArgumentError) Error() string { return e.Message }

// ErrMissingAuthenticationParam ports MissingAuthenticationException::forQueryParam().
func ErrMissingAuthenticationParam(param string) *ProblemError {
	return &ProblemError{
		Kind:       KindMissingAuth,
		Detail:     fmt.Sprintf(`Expected authentication to be provided in "%s" query param`, param),
		Title:      "Invalid authorization",
		Type:       ProblemType("missing-authentication"),
		Status:     http.StatusUnauthorized,
		Additional: []KV{{"param", param}},
	}
}
