package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

var startTime = time.Now()

// serveStatic ports RoadRunner's static middleware over the public directory (forbidding .php and .htaccess).
func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if a.Public == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	p := path.Clean(r.URL.Path)
	ext := strings.ToLower(path.Ext(p))
	if ext == ".php" || ext == ".htaccess" {
		return false
	}
	f, err := a.Public.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	if ext == ".ico" {
		w.Header().Set("Content-Type", "image/vnd.microsoft.icon")
	} else if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	modTime := info.ModTime()
	if modTime.IsZero() {
		modTime = startTime
	}
	http.ServeContent(w, r, info.Name(), modTime, f)
	return true
}

// parseFormBody ports RoadRunner's body parsing: url-encoded and multipart bodies are parsed into the request's
// parsed body; anything else is left raw.
func parseFormBody(r *http.Request, body []byte) (any, bool) {
	if r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return nil, false
	}
	ct, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/x-www-form-urlencoded":
		return ParseStr(string(body)), true
	case "multipart/form-data":
		mr, err := multipartValues(body, params["boundary"])
		if err != nil {
			return nil, false
		}
		return mr, true
	}
	return nil, false
}

func multipartValues(body []byte, boundary string) (map[string]any, error) {
	req := &http.Request{Header: http.Header{"Content-Type": {"multipart/form-data; boundary=" + boundary}},
		Body: io.NopCloser(bytes.NewReader(body)), Method: http.MethodPost}
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}
	var pairs []string
	for k, values := range req.MultipartForm.Value {
		for _, v := range values {
			pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return ParseStr(strings.Join(pairs, "&")), nil
}

// decodeJSON ports shlink-json's json_decode() with associative arrays: objects become maps, numbers json.Number.
func decodeJSON(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("malformed UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("syntax error")
	}
	return v, nil
}

// phpEmptyBody reports whether PHP's empty() is true for the body.
func phpEmptyBody(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// bodyParserMiddleware ports BodyParserMiddleware.
func bodyParserMiddleware(r *Request, next Handler) (*Response, error) {
	if !phpEmptyBody(r.Body) || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return next(r)
	}
	if len(r.RawBody) == 0 || string(r.RawBody) == "0" {
		return next(r)
	}
	parsed, err := decodeJSON(r.RawBody)
	if err != nil {
		return nil, core.ErrMalformedBody()
	}
	switch parsed.(type) {
	case map[string]any, []any:
	default:
		// shlink-json's json_decode() is declared to return an array, so null and scalars are type errors
		return nil, errors.New("type error: decoded JSON body is not an array")
	}
	r = r.Clone()
	r.Body = parsed
	return next(r)
}

// authenticationMiddleware ports AuthenticationMiddleware.
func (a *App) authenticationMiddleware(r *Request, next Handler) (*Response, error) {
	route := r.Route
	if route == nil || route.Failure || r.Method == http.MethodOptions {
		return next(r)
	}
	name := route.MatchedRouteName()
	if name == routeHealth || name == routeUnversionedHealth {
		return next(r)
	}
	var key string
	if name == routeSingleStepCreation {
		v := r.Query["apiKey"]
		if l, isList := v.([]any); isList && len(l) > 0 {
			return nil, errors.New("type error: apiKey must be a string")
		}
		if m, isMap := v.(map[string]any); isMap && len(m) > 0 {
			return nil, errors.New("type error: apiKey must be a string")
		}
		key, _ = v.(string)
		if key == "" || key == "0" {
			return nil, core.ErrMissingAuthenticationParam("apiKey")
		}
	} else {
		key = r.Header("X-Api-Key")
		if key == "" || key == "0" {
			return nil, core.ErrMissingAuthentication([]string{"X-Api-Key"})
		}
	}
	result, err := a.APIKeys.Check(r.HTTP.Context(), key)
	if err != nil {
		return nil, err
	}
	if !result.IsValid(a.Store.NowIn()) {
		return nil, core.ErrInvalidAPIKey()
	}
	r = r.Clone()
	r.APIKey = result.APIKey
	return next(r)
}

func action(h Handler) Middleware {
	return func(r *Request, _ Handler) (*Response, error) { return h(r) }
}

func withMiddlewares(final Middleware, mws ...Middleware) Middleware {
	return func(r *Request, outer Handler) (*Response, error) {
		h := func(r *Request) (*Response, error) { return final(r, outer) }
		return Chain(h, mws...)(r)
	}
}

// routes ports config/autoload/routes.config.php, with the BasePathPrefixer, MultiSegmentSlugProcessor and
// ShortUrlMethodsProcessor config post-processors applied.
func (a *App) routes() []*Route {
	get, post, patch, del, put := []string{"GET"}, []string{"POST"}, []string{"PATCH"}, []string{"DELETE"}, []string{"PUT"}
	drop := a.dropDefaultDomainMiddleware
	rest := []*Route{
		{Name: routeHealth, Path: "/health", Methods: get, Handler: action(a.healthAction)},
		{Name: "ShortUrlVisitsAction", Path: "/short-urls/{shortCode}/visits", Methods: get, Handler: withMiddlewares(action(a.shortURLVisitsAction), drop)},
		{Name: "DeleteShortUrlVisitsAction", Path: "/short-urls/{shortCode}/visits", Methods: del, Handler: withMiddlewares(action(a.deleteShortURLVisitsAction), drop)},
		{Name: "TagVisitsAction", Path: "/tags/{tag}/visits", Methods: get, Handler: action(a.tagVisitsAction)},
		{Name: "DomainVisitsAction", Path: "/domains/{domain}/visits", Methods: get, Handler: action(a.domainVisitsAction)},
		{Name: "GlobalVisitsAction", Path: "/visits", Methods: get, Handler: action(a.globalVisitsAction)},
		{Name: "OrphanVisitsAction", Path: "/visits/orphan", Methods: get, Handler: action(a.orphanVisitsAction)},
		{Name: "DeleteOrphanVisitsAction", Path: "/visits/orphan", Methods: del, Handler: action(a.deleteOrphanVisitsAction)},
		{Name: "NonOrphanVisitsAction", Path: "/visits/non-orphan", Methods: get, Handler: action(a.nonOrphanVisitsAction)},
		{Name: "ListRedirectRulesAction", Path: "/short-urls/{shortCode}/redirect-rules", Methods: get, Handler: withMiddlewares(action(a.listRedirectRulesAction), drop)},
		{Name: "SetRedirectRulesAction", Path: "/short-urls/{shortCode}/redirect-rules", Methods: []string{"POST", "PATCH"}, Handler: withMiddlewares(action(a.setRedirectRulesAction), drop)},
		{Name: "CreateShortUrlAction", Path: "/short-urls", Methods: post, Handler: withMiddlewares(action(a.createShortURLAction), drop, a.overrideDomainMiddleware)},
		{Name: routeSingleStepCreation, Path: "/short-urls/shorten", Methods: get, Handler: withMiddlewares(action(a.singleStepCreateShortURLAction), createShortURLContentNegotiationMiddleware, a.overrideDomainMiddleware)},
		{Name: "EditShortUrlAction", Path: "/short-urls/{shortCode}", Methods: patch, Handler: withMiddlewares(action(a.editShortURLAction), drop)},
		{Name: "DeleteShortUrlAction", Path: "/short-urls/{shortCode}", Methods: del, Handler: withMiddlewares(action(a.deleteShortURLAction), drop)},
		{Name: "ResolveShortUrlAction", Path: "/short-urls/{shortCode}", Methods: get, Handler: withMiddlewares(action(a.resolveShortURLAction), drop)},
		{Name: "ListShortUrlsAction", Path: "/short-urls", Methods: get, Handler: action(a.listShortURLsAction)},
		{Name: "ListTagsAction", Path: "/tags", Methods: get, Handler: action(a.listTagsAction)},
		{Name: "TagsStatsAction", Path: "/tags/stats", Methods: get, Handler: action(a.tagsStatsAction)},
		{Name: "DeleteTagsAction", Path: "/tags", Methods: del, Handler: action(a.deleteTagsAction)},
		{Name: "UpdateTagAction", Path: "/tags", Methods: put, Handler: action(a.updateTagAction)},
		{Name: "ListDomainsAction", Path: "/domains", Methods: get, Handler: action(a.listDomainsAction)},
		{Name: "DomainRedirectsAction", Path: "/domains/redirects", Methods: patch, Handler: action(a.domainRedirectsAction)},
		{Name: "MercureInfoAction", Path: "/mercure-info", Methods: get, Handler: action(a.mercureInfoAction)},
	}
	var routes []*Route
	for _, r := range rest {
		prefixed := *r
		prefixed.Path = "/rest/v{version:1|2|3}" + r.Path
		routes = append(routes, &prefixed)
	}
	unversioned := *rest[0]
	unversioned.Name, unversioned.Path = routeUnversionedHealth, "/rest/health"
	routes = append(routes, &unversioned)

	shortCodeSuffix := ""
	if a.Config.URLShortener.TrailingSlashEnabled {
		shortCodeSuffix = "[/]"
	}
	ipMws := []Middleware{a.ipAddressMiddleware, a.ipGeolocationMiddleware}
	routes = append(routes,
		&Route{Name: routeRobots, Path: "/robots.txt", Methods: get, Handler: action(a.robotsAction)},
		&Route{Name: routePixel, Path: "/{shortCode}/track", Methods: get, Handler: withMiddlewares(a.pixelAction, ipMws...)},
	)
	// ShortUrlMethodsProcessor moves the redirect route last and sets its methods from the redirect status
	routes = append(routes, &Route{
		Name: routeRedirect, Path: "/{shortCode}" + shortCodeSuffix, Methods: a.Config.Redirect.AllowedRedirectMethods(),
		Handler: withMiddlewares(a.redirectAction, append(ipMws, a.trimTrailingSlashMiddleware)...),
	})
	for _, r := range routes {
		if a.Config.URLShortener.MultiSegmentSlugsEnabled {
			r.Path = strings.ReplaceAll(r.Path, "{shortCode}", "{shortCode:.+}")
		}
		r.Path = a.Config.BasePath + r.Path
	}
	return routes
}

// healthAction ports HealthAction.
func (a *App) healthAction(r *Request) (*Response, error) {
	connected := a.Store.DB.DB.PingContext(r.HTTP.Context()) == nil
	if connected {
		_, err := a.Store.DB.DB.ExecContext(r.HTTP.Context(), "SELECT 1")
		connected = err == nil
	}
	status, code := "pass", http.StatusOK
	if !connected {
		status, code = "fail", http.StatusServiceUnavailable
	}
	resp := JSONResponse(phpx.Map{
		{Key: "status", Value: status},
		{Key: "version", Value: config.Version},
		{Key: "links", Value: phpx.Map{{Key: "about", Value: "https://shlink.io"}, {Key: "project", Value: "https://github.com/shlinkio/shlink"}}},
	}, code)
	resp.Header.Set("Content-Type", "application/health+json")
	return resp, nil
}

// mercureInfoAction ports MercureInfoAction, wrapped by NotConfiguredMercureErrorHandler.
func (a *App) mercureInfoAction(r *Request) (*Response, error) {
	var hub *string
	if a.Mercure != nil {
		hub = a.Mercure.PublicHubURL()
	}
	if hub == nil {
		a.Logger.Warn("This Shlink instance is not integrated with a mercure hub.")
		return problemResponseFromError(r, core.ErrMercureNotConfigured()), nil
	}
	expiresAt := a.Store.NowIn().AddDate(0, 0, a.Mercure.JWTDays())
	token, err := a.Mercure.BuildSubscriptionToken(expiresAt)
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{
		{Key: "mercureHubUrl", Value: *hub + "/.well-known/mercure"},
		{Key: "token", Value: token},
		{Key: "jwtExpiration", Value: phpx.AtomString(expiresAt)},
	}, http.StatusOK), nil
}

// dropDefaultDomainMiddleware ports DropDefaultDomainFromRequestMiddleware.
func (a *App) dropDefaultDomainMiddleware(r *Request, next Handler) (*Response, error) {
	def := a.Config.URLShortener.DefaultDomain
	sanitize := func(m map[string]any) map[string]any {
		if d, ok := m["domain"].(string); ok && d == def {
			out := make(map[string]any, len(m))
			for k, v := range m {
				if k != "domain" {
					out[k] = v
				}
			}
			return out
		}
		return m
	}
	r = r.Clone()
	r.Query = sanitize(r.Query)
	switch b := r.Body.(type) {
	case nil:
		r.Body = map[string]any{}
	case map[string]any:
		r.Body = sanitize(b)
	}
	return next(r)
}

// overrideDomainMiddleware ports OverrideDomainMiddleware.
func (a *App) overrideDomainMiddleware(r *Request, next Handler) (*Response, error) {
	role, ok := r.APIKey.Role(core.RoleDomainSpecific)
	if !ok {
		return next(r)
	}
	domain, err := a.Domains.GetDomain(r.HTTP.Context(), role.DomainID())
	if err != nil {
		return nil, err
	}
	r = r.Clone()
	authority := domain.Authority
	if r.Method == http.MethodPost {
		body := map[string]any{}
		for k, v := range r.BodyMap() {
			body[k] = v
		}
		body["domain"] = authority
		r.Body = body
		return next(r)
	}
	r.DomainOverride = &authority
	return next(r)
}

// createShortURLContentNegotiationMiddleware ports CreateShortUrlContentNegotiationMiddleware.
func createShortURLContentNegotiationMiddleware(r *Request, next Handler) (*Response, error) {
	resp, err := next(r)
	if err != nil || !resp.IsJSON {
		return resp, err
	}
	plain := false
	if format, ok := r.Query["format"]; ok && format != nil {
		s, _ := format.(string)
		plain = strings.ToLower(s) == "txt"
	} else {
		first, _, _ := strings.Cut(r.Header("Accept"), ",")
		plain = strings.Contains(strings.ToLower(first), "text/plain")
	}
	if !plain {
		return resp, nil
	}
	body := ""
	if m, ok := resp.Payload.(phpx.Map); ok {
		if v, ok := m.Get("shortUrl"); ok && v != nil {
			body, _ = v.(string)
		} else if v, ok := m.Get("error"); ok && v != nil {
			body, _ = v.(string)
		}
	}
	out := NewResponse(http.StatusOK)
	out.Header.Set("Content-Type", "text/plain")
	out.Body = []byte(body)
	return out, nil
}

// apiIdentifier ports ShortUrlIdentifier::fromApiRequest().
func apiIdentifier(r *Request) (core.ShortURLIdentifier, error) {
	var domain *string
	if v, ok := r.Query["domain"]; ok && v != nil {
		s, isString := v.(string)
		if !isString {
			return core.ShortURLIdentifier{}, errors.New("type error: domain must be a string")
		}
		domain = &s
	}
	return core.NewIdentifier(r.Params["shortCode"], domain), nil
}

func (a *App) transform(r *Request, su *core.ShortURL) (*Response, error) {
	deps, err := a.Store.WithDeps(r.HTTP.Context(), su)
	if err != nil {
		return nil, err
	}
	return JSONResponse(a.Transformer.Transform(deps), http.StatusOK), nil
}

func (a *App) shorten(r *Request, in valinor.Input, defaults core.CreationDefaults) (*Response, error) {
	creation, err := core.MapShortURLCreation(in, defaults, a.Store.Loc(), a.Store.NowIn())
	if err != nil {
		return nil, err
	}
	su, err := a.ShortURLs.Shorten(r.HTTP.Context(), creation)
	if err != nil {
		return nil, err
	}
	return a.transform(r, su)
}

// createShortURLAction ports CreateShortUrlAction, with ShortUrlOptionsPayloadMiddleware's defaults.
func (a *App) createShortURLAction(r *Request) (*Response, error) {
	o := a.Config.URLShortener
	return a.shorten(r, valinor.Input(r.BodyMap()), core.CreationDefaults{
		ShortURLMode: o.Mode, MultiSegmentSlugsEnabled: o.MultiSegmentSlugsEnabled,
		DefaultShortCodeLength: o.DefaultShortCodesLength, APIKey: r.APIKey,
	})
}

// singleStepCreateShortURLAction ports SingleStepCreateShortUrlAction: only longUrl and the forced domain are mapped,
// so every other option keeps ShortUrlCreation's constructor defaults.
func (a *App) singleStepCreateShortURLAction(r *Request) (*Response, error) {
	in := valinor.Input{"longUrl": r.Query["longUrl"], "domain": nil}
	if r.DomainOverride != nil {
		in["domain"] = *r.DomainOverride
	}
	return a.shorten(r, in, core.CreationDefaults{DefaultShortCodeLength: config.DefaultShortCodesLengthValue, APIKey: r.APIKey})
}

// editShortURLAction ports EditShortUrlAction.
func (a *App) editShortURLAction(r *Request) (*Response, error) {
	edition, err := core.MapShortURLEdition(valinor.Input(r.BodyMap()), a.Store.Loc(), a.Store.NowIn())
	if err != nil {
		return nil, err
	}
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	su, err := a.ShortURLs.UpdateShortURL(r.HTTP.Context(), id, edition, r.APIKey)
	if err != nil {
		return nil, err
	}
	return a.transform(r, su)
}

// deleteShortURLAction ports DeleteShortUrlAction.
func (a *App) deleteShortURLAction(r *Request) (*Response, error) {
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	if err := a.ShortURLs.DeleteByShortCode(r.HTTP.Context(), id, false, r.APIKey); err != nil {
		return nil, err
	}
	return EmptyResponse(), nil
}

// resolveShortURLAction ports ResolveShortUrlAction.
func (a *App) resolveShortURLAction(r *Request) (*Response, error) {
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	su, err := a.ShortURLs.ResolveShortURL(r.HTTP.Context(), id, r.APIKey)
	if err != nil {
		return nil, err
	}
	return a.transform(r, su)
}

// listShortURLsAction ports ListShortUrlsAction.
func (a *App) listShortURLsAction(r *Request) (*Response, error) {
	params, err := core.MapShortURLsParams(valinor.Input(r.Query), a.Store.Loc(), a.Store.NowIn())
	if err != nil {
		return nil, err
	}
	ctx := r.HTTP.Context()
	paginator, err := a.ShortURLs.ListShortURLs(ctx, params, r.APIKey)
	if err != nil {
		return nil, err
	}
	data, err := paginator.Serialize(ctx, func(d core.ShortURLWithDeps) any { return a.Transformer.Transform(d) }, "data")
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "shortUrls", Value: data}}, http.StatusOK), nil
}

func (a *App) visitsResponse(r *Request, paginator *core.Paginator[*core.Visit], err error) (*Response, error) {
	if err != nil {
		return nil, err
	}
	data, err := paginator.Serialize(r.HTTP.Context(), func(v *core.Visit) any { return v }, "data")
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "visits", Value: data}}, http.StatusOK), nil
}

func (a *App) visitsParams(r *Request, withType bool) (core.VisitsParams, error) {
	return core.VisitsParamsFromQuery(valinor.Input(r.Query), withType, a.Store.Loc(), a.Store.NowIn())
}

// shortURLVisitsAction ports ShortUrlVisitsAction.
func (a *App) shortURLVisitsAction(r *Request) (*Response, error) {
	p, err := a.visitsParams(r, false)
	if err != nil {
		return nil, err
	}
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	paginator, err := a.Visits.VisitsForShortURL(r.HTTP.Context(), id, p, r.APIKey)
	return a.visitsResponse(r, paginator, err)
}

// tagVisitsAction ports TagVisitsAction.
func (a *App) tagVisitsAction(r *Request) (*Response, error) {
	p, err := a.visitsParams(r, false)
	if err != nil {
		return nil, err
	}
	paginator, err := a.Visits.VisitsForTag(r.HTTP.Context(), r.Params["tag"], p, r.APIKey)
	return a.visitsResponse(r, paginator, err)
}

// domainVisitsAction ports DomainVisitsAction.
func (a *App) domainVisitsAction(r *Request) (*Response, error) {
	p, err := a.visitsParams(r, false)
	if err != nil {
		return nil, err
	}
	domain := r.Params["domain"]
	if domain == a.Config.URLShortener.DefaultDomain {
		domain = core.DefaultAuthority
	}
	paginator, err := a.Visits.VisitsForDomain(r.HTTP.Context(), domain, p, r.APIKey)
	return a.visitsResponse(r, paginator, err)
}

// globalVisitsAction ports GlobalVisitsAction.
func (a *App) globalVisitsAction(r *Request) (*Response, error) {
	stats, err := a.Visits.GetVisitsStats(r.HTTP.Context(), r.APIKey)
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "visits", Value: stats}}, http.StatusOK), nil
}

// orphanVisitsAction ports OrphanVisitsAction.
func (a *App) orphanVisitsAction(r *Request) (*Response, error) {
	p, err := a.visitsParams(r, true)
	if err != nil {
		return nil, err
	}
	paginator, err := a.Visits.OrphanVisits(r.HTTP.Context(), p, r.APIKey)
	return a.visitsResponse(r, paginator, err)
}

// nonOrphanVisitsAction ports NonOrphanVisitsAction.
func (a *App) nonOrphanVisitsAction(r *Request) (*Response, error) {
	p, err := a.visitsParams(r, false)
	if err != nil {
		return nil, err
	}
	paginator, err := a.Visits.NonOrphanVisits(r.HTTP.Context(), p, r.APIKey)
	return a.visitsResponse(r, paginator, err)
}

// deleteOrphanVisitsAction ports DeleteOrphanVisitsAction.
func (a *App) deleteOrphanVisitsAction(r *Request) (*Response, error) {
	n, err := a.Visits.DeleteOrphanVisits(r.HTTP.Context(), r.APIKey)
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "deletedVisits", Value: n}}, http.StatusOK), nil
}

// deleteShortURLVisitsAction ports DeleteShortUrlVisitsAction.
func (a *App) deleteShortURLVisitsAction(r *Request) (*Response, error) {
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	n, err := a.Visits.DeleteShortURLVisits(r.HTTP.Context(), id, r.APIKey)
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "deletedVisits", Value: n}}, http.StatusOK), nil
}

// tagsParams ports TagsParams::fromRawData(); non-string values hit PHP type errors.
func tagsParams(q map[string]any) (core.TagsParams, error) {
	var p core.TagsParams
	str := func(key string) (*string, error) {
		v, ok := q[key]
		if !ok || v == nil {
			return nil, nil
		}
		s, isString := v.(string)
		if !isString {
			return nil, errors.New("type error: " + key + " must be a string")
		}
		return &s, nil
	}
	var err error
	if p.SearchTerm, err = str("searchTerm"); err != nil {
		return p, err
	}
	orderBy, err := str("orderBy")
	if err != nil {
		return p, err
	}
	p.OrderBy = core.OrderingFromOptionalString(orderBy)
	p.Page = 1
	if v, ok := q["page"]; ok && v != nil {
		if n := phpIntCast(v); n > 0 {
			p.Page = n
		}
	}
	p.ItemsPerPage = -1
	if v, ok := q["itemsPerPage"]; ok && v != nil {
		if n := phpIntCast(v); n >= 0 {
			p.ItemsPerPage = n
		}
	}
	return p, nil
}

// listTagsAction ports ListTagsAction.
func (a *App) listTagsAction(r *Request) (*Response, error) {
	p, err := tagsParams(r.Query)
	if err != nil {
		return nil, err
	}
	ctx := r.HTTP.Context()
	paginator, err := a.Tags.ListTags(ctx, p, r.APIKey)
	if err != nil {
		return nil, err
	}
	data, err := paginator.Serialize(ctx, func(s string) any { return s }, "data")
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "tags", Value: data}}, http.StatusOK), nil
}

// tagsStatsAction ports TagsStatsAction.
func (a *App) tagsStatsAction(r *Request) (*Response, error) {
	p, err := tagsParams(r.Query)
	if err != nil {
		return nil, err
	}
	ctx := r.HTTP.Context()
	paginator, err := a.Tags.TagsInfo(ctx, p, r.APIKey)
	if err != nil {
		return nil, err
	}
	data, err := paginator.Serialize(ctx, func(t core.TagInfo) any { return t }, "data")
	if err != nil {
		return nil, err
	}
	return JSONResponse(phpx.Map{{Key: "tags", Value: data}}, http.StatusOK), nil
}

// deleteTagsAction ports DeleteTagsAction.
func (a *App) deleteTagsAction(r *Request) (*Response, error) {
	var names []string
	if v, ok := r.Query["tags"]; ok && v != nil {
		switch t := v.(type) {
		case []any:
			for _, item := range t {
				s, _ := item.(string)
				names = append(names, s)
			}
		case map[string]any:
			for _, k := range sortedMapKeys(t) {
				s, _ := t[k].(string)
				names = append(names, s)
			}
		default:
			return nil, errors.New("type error: tags must be an array")
		}
	}
	if err := a.Tags.DeleteTags(r.HTTP.Context(), names, r.APIKey); err != nil {
		return nil, err
	}
	return EmptyResponse(), nil
}

// updateTagAction ports UpdateTagAction and Renaming::fromArray().
func (a *App) updateTagAction(r *Request) (*Response, error) {
	body := r.BodyMap()
	if body["oldName"] == nil || body["newName"] == nil {
		return nil, core.ErrValidation("oldName", "newName")
	}
	oldName, ok1 := body["oldName"].(string)
	newName, ok2 := body["newName"].(string)
	if !ok1 || !ok2 {
		return nil, errors.New("type error: tag names must be strings")
	}
	if err := a.Tags.RenameTag(r.HTTP.Context(), core.Renaming{OldName: oldName, NewName: newName}, r.APIKey); err != nil {
		return nil, err
	}
	return EmptyResponse(), nil
}

// listDomainsAction ports ListDomainsAction.
func (a *App) listDomainsAction(r *Request) (*Response, error) {
	items, err := a.Domains.ListDomains(r.HTTP.Context(), r.APIKey)
	if err != nil {
		return nil, err
	}
	data := make([]any, len(items))
	for i, it := range items {
		data[i] = it
	}
	return JSONResponse(phpx.Map{{Key: "domains", Value: phpx.Map{
		{Key: "data", Value: data},
		{Key: "defaultRedirects", Value: core.NotFoundRedirectsFromOptions(a.Config.NotFoundRedirects)},
	}}}, http.StatusOK), nil
}

// domainRedirectsAction ports DomainRedirectsAction.
func (a *App) domainRedirectsAction(r *Request) (*Response, error) {
	req, err := core.MapDomainRedirectsRequest(valinor.Input(r.BodyMap()))
	if err != nil {
		return nil, err
	}
	ctx := r.HTTP.Context()
	domain, err := a.Domains.GetOrCreate(ctx, req.Authority, nil)
	if err != nil {
		return nil, err
	}
	redirects := req.ToNotFoundRedirects(domain)
	if _, err := a.Domains.ConfigureNotFoundRedirects(ctx, req.Authority, redirects, r.APIKey); err != nil {
		return nil, err
	}
	return JSONResponse(redirects, http.StatusOK), nil
}

func (a *App) resolveForRules(r *Request) (*core.ShortURL, error) {
	id, err := apiIdentifier(r)
	if err != nil {
		return nil, err
	}
	return a.ShortURLs.ResolveShortURL(r.HTTP.Context(), id, r.APIKey)
}

func rulesResponse(su *core.ShortURL, rules []core.RedirectRule) *Response {
	list := make([]any, len(rules))
	for i, rule := range rules {
		list[i] = rule
	}
	return JSONResponse(phpx.Map{{Key: "defaultLongUrl", Value: su.LongURL}, {Key: "redirectRules", Value: list}}, http.StatusOK)
}

// listRedirectRulesAction ports ListRedirectRulesAction.
func (a *App) listRedirectRulesAction(r *Request) (*Response, error) {
	su, err := a.resolveForRules(r)
	if err != nil {
		return nil, err
	}
	rules, err := a.Store.RulesForShortURL(r.HTTP.Context(), su.ID)
	if err != nil {
		return nil, err
	}
	return rulesResponse(su, rules), nil
}

// setRedirectRulesAction ports SetRedirectRulesAction.
func (a *App) setRedirectRulesAction(r *Request) (*Response, error) {
	su, err := a.resolveForRules(r)
	if err != nil {
		return nil, err
	}
	data, err := core.MapRedirectRulesData(valinor.Input(r.BodyMap()))
	if err != nil {
		return nil, err
	}
	rules, err := a.Store.SetRulesForShortURL(r.HTTP.Context(), su.ID, data)
	if err != nil {
		return nil, err
	}
	return rulesResponse(su, rules), nil
}

// phpIntCast ports PHP's (int) cast for query values: strings by their numeric prefix, arrays by emptiness.
func phpIntCast(v any) int {
	switch t := v.(type) {
	case string:
		return config.PHPIntCast(t)
	case []any:
		if len(t) > 0 {
			return 1
		}
	case map[string]any:
		if len(t) > 0 {
			return 1
		}
	}
	return 0
}
