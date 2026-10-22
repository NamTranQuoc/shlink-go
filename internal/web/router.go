package web

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Route ports Mezzio\Router\Route. A nil Methods slice allows any method.
type Route struct {
	Name    string
	Path    string
	Methods []string
	// Handler is the route's middleware pipe; it may delegate to the rest of the application pipeline.
	Handler Middleware
}

func (r *Route) allowsMethod(method string) bool {
	if r.Methods == nil {
		return true
	}
	method = strings.ToUpper(method)
	for _, m := range r.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// RouteResult ports Mezzio\Router\RouteResult. AllowedMethods is nil for "any method" failures.
type RouteResult struct {
	Route          *Route
	Params         map[string]string
	Failure        bool
	AllowedMethods []string
}

// IsMethodFailure ports RouteResult::isMethodFailure().
func (r *RouteResult) IsMethodFailure() bool { return r.Failure && r.AllowedMethods != nil }

// MatchedRouteName returns the matched route's name, or "".
func (r *RouteResult) MatchedRouteName() string {
	if r.Route == nil {
		return ""
	}
	return r.Route.Name
}

// httpMethodsStandard ports FastRouteRouter::HTTP_METHODS_STANDARD, used for routes allowing any method.
var httpMethodsStandard = []string{"HEAD", "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE"}

type variableRoute struct {
	regex   *regexp.Regexp
	vars    []string
	handler string
}

// Router ports Mezzio's FastRouteRouter over FastRoute's GroupCountBased dispatcher: static routes win over variable
// ones, variable routes match in registration order, HEAD falls back to GET, and a miss reports the methods that would
// have matched.
type Router struct {
	routes        []*Route
	staticMethods []string
	static        map[string]map[string]string
	varMethods    []string
	variable      map[string][]variableRoute
}

// NewRouter creates an empty router.
func NewRouter() *Router {
	return &Router{static: map[string]map[string]string{}, variable: map[string][]variableRoute{}}
}

var placeholderRe = regexp.MustCompile(`\{\s*([a-zA-Z_][a-zA-Z0-9_-]*)\s*(?::\s*([^{}]*(?:\{[^{}]*\}[^{}]*)*))?\}`)

// parseRoute ports FastRoute\RouteParser\Std::parse(): one entry per optional-segment expansion; each part is either
// a literal string or a [name, regex] placeholder.
func parseRoute(route string) ([][]any, error) {
	withoutClosing := strings.TrimRight(route, "]")
	numOptionals := len(route) - len(withoutClosing)
	// Split on '[' outside placeholders
	var segments []string
	depth, start := 0, 0
	for i := 0; i < len(withoutClosing); i++ {
		switch withoutClosing[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '[':
			if depth == 0 {
				segments = append(segments, withoutClosing[start:i])
				start = i + 1
			}
		}
	}
	segments = append(segments, withoutClosing[start:])
	if numOptionals != len(segments)-1 {
		return nil, fmt.Errorf("number of opening '[' and closing ']' does not match in %q", route)
	}
	var out [][]any
	current := ""
	for n, seg := range segments {
		if seg == "" && n != 0 {
			return nil, fmt.Errorf("empty optional part in %q", route)
		}
		current += seg
		out = append(out, parsePlaceholders(current))
	}
	return out, nil
}

func parsePlaceholders(route string) []any {
	matches := placeholderRe.FindAllStringSubmatchIndex(route, -1)
	if matches == nil {
		return []any{route}
	}
	var parts []any
	offset := 0
	for _, m := range matches {
		if m[0] > offset {
			parts = append(parts, route[offset:m[0]])
		}
		name := route[m[2]:m[3]]
		regex := "[^/]+"
		if m[4] >= 0 {
			regex = strings.TrimSpace(route[m[4]:m[5]])
		}
		parts = append(parts, [2]string{name, regex})
		offset = m[1]
	}
	if offset != len(route) {
		parts = append(parts, route[offset:])
	}
	return parts
}

// AddRoute registers a route, like FastRouteRouter::injectRoute().
func (r *Router) AddRoute(route *Route) error {
	r.routes = append(r.routes, route)
	methods := route.Methods
	if methods == nil {
		methods = httpMethodsStandard
	}
	datas, err := parseRoute(route.Path)
	if err != nil {
		return err
	}
	for _, method := range methods {
		for _, data := range datas {
			if len(data) == 1 {
				if s, ok := data[0].(string); ok {
					if err := r.addStatic(method, s, route.Path); err != nil {
						return err
					}
					continue
				}
			}
			if err := r.addVariable(method, data, route.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Router) addStatic(method, path, handler string) error {
	if _, ok := r.static[method]; !ok {
		r.static[method] = map[string]string{}
		r.staticMethods = append(r.staticMethods, method)
	}
	if _, exists := r.static[method][path]; exists {
		return fmt.Errorf(`cannot register two routes matching "%s" for method "%s"`, path, method)
	}
	for _, v := range r.variable[method] {
		if v.regex.MatchString(path) {
			return fmt.Errorf(`static route "%s" is shadowed by previously defined variable route for method "%s"`, path, method)
		}
	}
	r.static[method][path] = handler
	return nil
}

func (r *Router) addVariable(method string, data []any, handler string) error {
	var b strings.Builder
	var vars []string
	for _, part := range data {
		if s, ok := part.(string); ok {
			b.WriteString(regexp.QuoteMeta(s))
			continue
		}
		ph := part.([2]string)
		vars = append(vars, ph[0])
		b.WriteString("(" + ph[1] + ")")
	}
	// PCRE's "$" also matches before a trailing newline
	re, err := regexp.Compile(`^(?:` + b.String() + `)\n?$`)
	if err != nil {
		return err
	}
	if _, ok := r.variable[method]; !ok {
		r.varMethods = append(r.varMethods, method)
	}
	for _, v := range r.variable[method] {
		if v.regex.String() == re.String() {
			return fmt.Errorf(`cannot register two routes matching "%s" for method "%s"`, b.String(), method)
		}
	}
	r.variable[method] = append(r.variable[method], variableRoute{regex: re, vars: vars, handler: handler})
	return nil
}

func (r *Router) dispatchVariable(method, uri string) (string, map[string]string, bool) {
	for _, v := range r.variable[method] {
		m := v.regex.FindStringSubmatch(uri)
		if m == nil {
			continue
		}
		params := make(map[string]string, len(v.vars))
		for i, name := range v.vars {
			params[name] = m[i+1]
		}
		return v.handler, params, true
	}
	return "", nil, false
}

const (
	dispatchNotFound = iota
	dispatchFound
	dispatchMethodNotAllowed
)

// dispatch ports FastRoute\Dispatcher\RegexBasedAbstract::dispatch().
func (r *Router) dispatch(method, uri string) (int, string, map[string]string, []string) {
	if h, ok := r.static[method][uri]; ok {
		return dispatchFound, h, map[string]string{}, nil
	}
	if h, params, ok := r.dispatchVariable(method, uri); ok {
		return dispatchFound, h, params, nil
	}
	if method == "HEAD" {
		if h, ok := r.static["GET"][uri]; ok {
			return dispatchFound, h, map[string]string{}, nil
		}
		if h, params, ok := r.dispatchVariable("GET", uri); ok {
			return dispatchFound, h, params, nil
		}
	}
	if h, ok := r.static["*"][uri]; ok {
		return dispatchFound, h, map[string]string{}, nil
	}
	if h, params, ok := r.dispatchVariable("*", uri); ok {
		return dispatchFound, h, params, nil
	}
	var allowed []string
	for _, m := range r.staticMethods {
		if _, ok := r.static[m][uri]; ok && m != method {
			allowed = append(allowed, m)
		}
	}
	for _, m := range r.varMethods {
		if m == method {
			continue
		}
		if _, _, ok := r.dispatchVariable(m, uri); ok {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) > 0 {
		return dispatchMethodNotAllowed, "", nil, allowed
	}
	return dispatchNotFound, "", nil, nil
}

// Match ports FastRouteRouter::match().
func (r *Router) Match(method string, uri phpx.URI) *RouteResult {
	path := phpx.RawURLDecode(uri.Path)
	status, handler, params, allowed := r.dispatch(method, path)
	switch status {
	case dispatchMethodNotAllowed:
		return &RouteResult{Failure: true, AllowedMethods: allowed}
	case dispatchNotFound:
		return &RouteResult{Failure: true}
	}
	for _, route := range r.routes {
		if route.Path == handler && route.allowsMethod(method) {
			return &RouteResult{Route: route, Params: params}
		}
	}
	// marshalMethodNotAllowedResult(): collect the methods of every route with the same path
	var methods []string
	seen := map[string]bool{}
	for _, route := range r.routes {
		if route.Path != handler {
			continue
		}
		// (array) null contributes nothing for routes allowing any method
		for _, m := range route.Methods {
			if !seen[m] {
				seen[m] = true
				methods = append(methods, m)
			}
		}
	}
	return &RouteResult{Failure: true, AllowedMethods: methods}
}
