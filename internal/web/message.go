package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Request is the PSR-7 server request view of an HTTP request, plus the attributes middlewares attach to it.
type Request struct {
	HTTP *http.Request
	// Method may differ from HTTP.Method when ImplicitHeadMiddleware re-dispatches HEAD as GET.
	Method string
	// URI is the request URI as RoadRunner builds it (scheme, Host header, path and query).
	URI phpx.URI
	// Query holds the parsed query params (parse_str semantics).
	Query map[string]any
	// Body holds the parsed body: nil, a form payload or a decoded JSON value.
	Body       any
	RawBody    []byte
	RemoteAddr string

	Route     *RouteResult
	Params    map[string]string
	APIKey    *core.APIKey
	IP        *string
	Location  *core.Location
	RequestID string
	// DomainOverride is set by OverrideDomainMiddleware on non-POST requests.
	DomainOverride *string
	NotFoundType   *NotFoundType
	// ForwardedHead is set when ImplicitHeadMiddleware dispatched a HEAD request as GET.
	ForwardedHead bool
}

// Header ports getHeaderLine(): every value of the header joined by ", ".
func (r *Request) Header(name string) string {
	return strings.Join(r.HTTP.Header.Values(name), ", ")
}

// HasHeader ports hasHeader().
func (r *Request) HasHeader(name string) bool {
	return len(r.HTTP.Header.Values(name)) > 0
}

// Path returns the URI path as PSR-7 exposes it (percent-encoded).
func (r *Request) Path() string { return r.URI.Path }

// Clone returns a shallow copy, mirroring PSR-7's with* immutability.
func (r *Request) Clone() *Request {
	c := *r
	return &c
}

// BodyMap returns the parsed body as an input map, like the array actions read from getParsedBody().
func (r *Request) BodyMap() map[string]any {
	if m, ok := r.Body.(map[string]any); ok {
		return m
	}
	if l, ok := r.Body.([]any); ok {
		m := make(map[string]any, len(l))
		for i, v := range l {
			m[itoa(i)] = v
		}
		return m
	}
	return map[string]any{}
}

// QueryString returns a query param when it is a plain string.
func (r *Request) QueryString(key string) (string, bool) {
	v, ok := r.Query[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// Response is a buffered PSR-7 response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	// Payload is set for JSON responses, like JsonResponse::getPayload().
	Payload any
	IsJSON  bool
	// Streamed marks bodies upstream produces from a generator: their size is unknown, so no Content-Length is set
	// and they are sent chunked.
	Streamed bool
}

// NewResponse creates an empty response with given status.
func NewResponse(status int) *Response {
	return &Response{Status: status, Header: http.Header{}}
}

// EmptyResponse ports Diactoros' EmptyResponse (204 by default).
func EmptyResponse() *Response { return NewResponse(http.StatusNoContent) }

// JSONResponse ports Diactoros' JsonResponse with its default encoding flags.
func JSONResponse(payload any, status int) *Response {
	resp := NewResponse(status)
	resp.Header.Set("Content-Type", "application/json")
	resp.Body = phpx.Encode(payload, phpx.DiactorosJSONFlags)
	resp.Payload = payload
	resp.IsJSON = true
	return resp
}

// TextResponse builds a text/plain response.
func TextResponse(body string, status int) *Response {
	resp := NewResponse(status)
	resp.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp.Body = []byte(body)
	return resp
}

// HTMLResponse ports Diactoros' HtmlResponse.
func HTMLResponse(body string, status int) *Response {
	resp := NewResponse(status)
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp.Body = []byte(body)
	return resp
}

// RedirectResponse ports Diactoros' RedirectResponse.
func RedirectResponse(location string, status int) *Response {
	resp := NewResponse(status)
	resp.Header.Set("Location", location)
	return resp
}

// Handler handles a request; errors play the role of PHP exceptions.
type Handler func(*Request) (*Response, error)

// Middleware wraps a handler.
type Middleware func(*Request, Handler) (*Response, error)

// Chain composes middlewares in front of a final handler.
func Chain(final Handler, mws ...Middleware) Handler {
	h := final
	for i := len(mws) - 1; i >= 0; i-- {
		mw, next := mws[i], h
		h = func(r *Request) (*Response, error) { return mw(r, next) }
	}
	return h
}

func itoa(i int) string { return strconv.Itoa(i) }
