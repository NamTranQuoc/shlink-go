package web

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

//go:embed assets/404.html
var notFoundTemplate string

//go:embed assets/invalid-short-code.html
var invalidShortCodeTemplate string

// Route names, ported from the upstream action class names they identify.
const (
	routeHealth             = "HealthAction"
	routeUnversionedHealth  = "unversioned_health"
	routeRedirect           = "RedirectAction"
	routePixel              = "PixelAction"
	routeRobots             = "RobotsAction"
	routeSingleStepCreation = "SingleStepCreateShortUrlAction"
)

// NotFoundType ports ErrorHandler\Model\NotFoundType; a nil Type means none applies.
type NotFoundType struct {
	Type *core.VisitType
}

func (t *NotFoundType) is(v core.VisitType) bool { return t != nil && t.Type != nil && *t.Type == v }

// IsBaseURL ports NotFoundType::isBaseUrl().
func (t *NotFoundType) IsBaseURL() bool { return t.is(core.VisitBaseURL) }

// IsRegularNotFound ports NotFoundType::isRegularNotFound().
func (t *NotFoundType) IsRegularNotFound() bool { return t.is(core.VisitRegular404) }

// IsInvalidShortURL ports NotFoundType::isInvalidShortUrl().
func (t *NotFoundType) IsInvalidShortURL() bool { return t.is(core.VisitInvalidShortURL) }

func notFoundTypeFromRequest(r *Request, basePath string) *NotFoundType {
	result := r.Route
	if result == nil {
		result = &RouteResult{Failure: true}
	}
	var typ core.VisitType
	switch {
	case strings.TrimRight(r.URI.Path, "/") == basePath:
		typ = core.VisitBaseURL
	case result.Failure:
		typ = core.VisitRegular404
	case result.MatchedRouteName() == routeRedirect:
		typ = core.VisitInvalidShortURL
	default:
		return &NotFoundType{}
	}
	return &NotFoundType{Type: &typ}
}

// ipAddressMiddleware ports RKA\Middleware\IpAddress.
func (a *App) ipAddressMiddleware(r *Request, next Handler) (*Response, error) {
	r = r.Clone()
	r.IP = a.IP.Resolve(r.RemoteAddr, r.Header)
	return next(r)
}

// ipGeolocationMiddleware ports IpGeolocationMiddleware.
func (a *App) ipGeolocationMiddleware(r *Request, next Handler) (*Response, error) {
	if !a.Config.Tracking.IsGeolocationRelevant() {
		return next(r)
	}
	if a.Geo == nil || !a.Geo.DatabaseFileExists() {
		a.Logger.Warn("Tried to geolocate IP address, but a GeoLite2 db was not found.")
		return next(r)
	}
	r = r.Clone()
	r.Location = &core.Location{}
	if r.IP != nil && *r.IP != "127.0.0.1" {
		loc, err := a.Geo.ResolveIPLocation(*r.IP)
		if err != nil {
			a.Logger.Warn("Tried to locate IP address, but it seems to be wrong.", "e", err)
		} else {
			r.Location = loc
		}
	}
	return next(r)
}

// trimTrailingSlashMiddleware ports TrimTrailingSlashMiddleware.
func (a *App) trimTrailingSlashMiddleware(r *Request, next Handler) (*Response, error) {
	if sc, ok := r.Params["shortCode"]; ok && a.Config.URLShortener.TrailingSlashEnabled {
		r = r.Clone()
		params := make(map[string]string, len(r.Params))
		for k, v := range r.Params {
			params[k] = v
		}
		params["shortCode"] = strings.TrimRight(sc, "/")
		r.Params = params
	}
	return next(r)
}

func (a *App) shouldTrackRequest(r *Request) bool {
	if r.ForwardedHead {
		return false
	}
	o := a.Config.Tracking
	if r.IP != nil && len(o.DisableTrackingFrom) > 0 {
		if matches, ok := core.IPAddressMatchesGroups(*r.IP, o.DisableTrackingFrom); ok && matches {
			return false
		}
	}
	if o.DisableTrackParam != nil {
		if _, ok := r.Query[*o.DisableTrackParam]; ok {
			return false
		}
	}
	return true
}

func (a *App) visitorFromRequest(r *Request, redirectURL *string) core.Visitor {
	return core.NewVisitor(r.Header("User-Agent"), r.Header("Referer"), r.IP, r.URI.String(), r.Location, redirectURL)
}

// trackIfApplicable ports RequestTracker::trackIfApplicable().
func (a *App) trackIfApplicable(ctx context.Context, su *core.ShortURL, r *Request, redirectURL *string) error {
	if !a.shouldTrackRequest(r) {
		return nil
	}
	_, err := a.Tracker.Track(ctx, su, a.visitorFromRequest(r, redirectURL))
	return err
}

// trackNotFoundIfApplicable ports RequestTracker::trackNotFoundIfApplicable().
func (a *App) trackNotFoundIfApplicable(ctx context.Context, r *Request, redirectURL *string) error {
	if !a.shouldTrackRequest(r) || r.NotFoundType == nil || r.NotFoundType.Type == nil {
		return nil
	}
	_, err := a.Tracker.TrackOrphan(ctx, *r.NotFoundType.Type, a.visitorFromRequest(r, redirectURL))
	return err
}

func locationHeader(resp *Response) *string {
	if values := resp.Header.Values("Location"); len(values) > 0 {
		v := strings.Join(values, ", ")
		return &v
	}
	return nil
}

// ruleRequest builds the data redirect conditions are evaluated against.
func (a *App) ruleRequest(r *Request) *core.RuleRequest {
	return &core.RuleRequest{
		Query:          r.Query,
		AcceptLanguage: r.Header("Accept-Language"),
		UserAgent:      r.Header("User-Agent"),
		RemoteAddress:  r.IP,
		Geolocation:    r.Location,
		Now:            a.Store.NowIn(),
	}
}

// buildShortURLRedirect ports ShortUrlRedirectionBuilder::buildShortUrlRedirect().
func (a *App) buildShortURLRedirect(ctx context.Context, su *core.ShortURL, r *Request, extraPath *string) (string, error) {
	longURL, err := a.Store.ResolveLongURL(ctx, su, a.ruleRequest(r))
	if err != nil {
		return "", err
	}
	uri, ok := phpx.ParseURI(longURL)
	if !ok {
		return "", fmt.Errorf("unable to parse URI: %s", longURL)
	}
	query := uri.Query
	if su.ForwardQuery {
		current := phpx.ParseQuery(r.URI.Query)
		if p := a.Config.Tracking.DisableTrackParam; p != nil {
			current = current.Without(*p)
		}
		query = phpx.BuildQuery(phpx.MergeQueries(phpx.ParseQuery(uri.Query), current))
	}
	path := uri.Path
	if extraPath != nil {
		path += *extraPath
	}
	return uri.WithQuery(query).WithPath(path).String(), nil
}

// buildRedirectResponse ports RedirectResponseHelper::buildRedirectResponse().
func (a *App) buildRedirectResponse(location string) *Response {
	o := a.Config.Redirect
	resp := RedirectResponse(location, o.StatusCode)
	if o.StatusCode == 301 || o.StatusCode == 308 {
		resp.Header.Set("Cache-Control", fmt.Sprintf("%s,max-age=%d", o.CacheVisibility, o.CacheLifetime))
	}
	return resp
}

func (a *App) redirectIdentifier(r *Request) core.ShortURLIdentifier {
	authority := r.URI.Authority()
	return core.NewIdentifier(r.Params["shortCode"], &authority)
}

// redirectAction ports RedirectAction.
func (a *App) redirectAction(r *Request, next Handler) (*Response, error) {
	ctx := r.HTTP.Context()
	su, err := a.ShortURLs.ResolveEnabledShortURL(ctx, a.redirectIdentifier(r))
	if core.IsKind(err, core.KindShortURLNotFound) {
		return next(r)
	}
	if err != nil {
		return nil, err
	}
	longURL, err := a.buildShortURLRedirect(ctx, su, r, nil)
	if err != nil {
		return nil, err
	}
	resp := a.buildRedirectResponse(longURL)
	if err := a.trackIfApplicable(ctx, su, r, locationHeader(resp)); err != nil {
		return nil, err
	}
	return resp, nil
}

var pixelGIF, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAJAAAP8AAAAAACH5BAUQAAAALAAAAAABAAEAAAICBAEAOw==")

func pixelResponse() *Response {
	resp := NewResponse(http.StatusOK)
	resp.Header.Set("Content-Type", "image/gif")
	resp.Body = pixelGIF
	return resp
}

// pixelAction ports PixelAction.
func (a *App) pixelAction(r *Request, _ Handler) (*Response, error) {
	ctx := r.HTTP.Context()
	su, err := a.ShortURLs.ResolveEnabledShortURL(ctx, a.redirectIdentifier(r))
	if core.IsKind(err, core.KindShortURLNotFound) {
		return pixelResponse(), nil
	}
	if err != nil {
		return nil, err
	}
	resp := pixelResponse()
	if err := a.trackIfApplicable(ctx, su, r, nil); err != nil {
		return nil, err
	}
	return resp, nil
}

// robotsAction ports RobotsAction.
func (a *App) robotsAction(r *Request) (*Response, error) {
	var b strings.Builder
	b.WriteString("# For more information about the robots.txt standard, see:\n# https://www.robotstxt.org/orig.html\n\n")
	agents := a.Config.Robots.UserAgents
	if len(agents) == 0 {
		agents = []string{"*"}
	}
	for _, ua := range agents {
		fmt.Fprintf(&b, "User-agent: %s\n", ua)
	}
	if a.Config.Robots.AllowAllShortURLs {
		b.WriteString("Disallow: /rest/")
	} else {
		codes, err := a.Store.CrawlableShortCodes(r.HTTP.Context())
		if err != nil {
			return nil, err
		}
		for _, c := range codes {
			fmt.Fprintf(&b, "Allow: /%s\n", c)
		}
		b.WriteString("Disallow: /")
	}
	resp := NewResponse(http.StatusOK)
	resp.Header.Set("Content-Type", "text/plain")
	resp.Body = []byte(b.String())
	resp.Streamed = true
	return resp, nil
}

// notFoundTypeResolverMiddleware ports NotFoundTypeResolverMiddleware.
func (a *App) notFoundTypeResolverMiddleware(r *Request, next Handler) (*Response, error) {
	r = r.Clone()
	r.NotFoundType = notFoundTypeFromRequest(r, a.Config.BasePath)
	return next(r)
}

// extraPathRedirectMiddleware ports ExtraPathRedirectMiddleware.
func (a *App) extraPathRedirectMiddleware(r *Request, next Handler) (*Response, error) {
	o := a.Config.URLShortener
	t := r.NotFoundType
	if t == nil || o.ExtraPathMode == config.ExtraPathDefault {
		return next(r)
	}
	if !((o.MultiSegmentSlugsEnabled && t.IsInvalidShortURL()) || (!o.MultiSegmentSlugsEnabled && t.IsRegularNotFound())) {
		return next(r)
	}
	ctx := r.HTTP.Context()
	parts := strings.Split(strings.Trim(r.URI.Path, "/"), "/")
	authority := r.URI.Authority()
	for segments := 1; ; segments++ {
		shortCode := strings.Join(parts[:segments], "/")
		var extraPath *string
		if rest := parts[segments:]; len(rest) > 0 {
			p := "/" + strings.Join(rest, "/")
			extraPath = &p
		}
		su, err := a.ShortURLs.ResolveEnabledShortURL(ctx, core.NewIdentifier(shortCode, &authority))
		if core.IsKind(err, core.KindShortURLNotFound) {
			if extraPath == nil || !o.MultiSegmentSlugsEnabled {
				return next(r)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var appended *string
		if o.ExtraPathMode == config.ExtraPathAppend {
			appended = extraPath
		}
		longURL, err := a.buildShortURLRedirect(ctx, su, r, appended)
		if err != nil {
			return nil, err
		}
		if err := a.trackIfApplicable(ctx, su, r, &longURL); err != nil {
			return nil, err
		}
		return a.buildRedirectResponse(longURL), nil
	}
}

// notFoundTrackerMiddleware ports NotFoundTrackerMiddleware.
func (a *App) notFoundTrackerMiddleware(r *Request, next Handler) (*Response, error) {
	resp, err := next(r)
	if err != nil {
		return nil, err
	}
	if err := a.trackNotFoundIfApplicable(r.HTTP.Context(), r, locationHeader(resp)); err != nil {
		return nil, err
	}
	return resp, nil
}

// notFoundRedirectHandler ports NotFoundRedirectHandler.
func (a *App) notFoundRedirectHandler(r *Request, next Handler) (*Response, error) {
	configs, err := a.Domains.NotFoundRedirectConfigs(r.HTTP.Context(), r.URI.Authority())
	if err != nil {
		return nil, err
	}
	for _, c := range configs {
		if resp := a.resolveNotFoundRedirect(r.NotFoundType, c, r.URI); resp != nil {
			return resp, nil
		}
	}
	return next(r)
}

// resolveNotFoundRedirect ports NotFoundRedirectResolver::resolveRedirectResponse().
func (a *App) resolveNotFoundRedirect(t *NotFoundType, c core.NotFoundRedirects, current phpx.URI) *Response {
	var target *string
	switch {
	case t.IsBaseURL():
		target = c.BaseURLRedirect
	case t.IsRegularNotFound():
		target = c.Regular404Redirect
	case t.IsInvalidShortURL():
		target = c.InvalidShortURLRedirect
	}
	if target == nil {
		return nil
	}
	return a.buildRedirectResponse(a.resolvePlaceholders(current, *target))
}

func (a *App) resolvePlaceholders(current phpx.URI, redirectURL string) string {
	uri, ok := phpx.ParseURI(redirectURL)
	if !ok {
		a.Logger.Warn(fmt.Sprintf(`It was not possible to parse "%s" as a valid URL`, redirectURL))
		return redirectURL
	}
	path, domain := current.Path, current.Authority()
	replace := func(modifier func(string) string, value string) string {
		value = strings.ReplaceAll(value, phpx.URLEncode("{DOMAIN}"), modifier(domain))
		return strings.ReplaceAll(value, phpx.URLEncode("{ORIGINAL_PATH}"), modifier(path))
	}
	newPath := strings.ReplaceAll(replace(func(v string) string { return v }, uri.Path), "//", "/")
	newQuery := replace(phpx.URLEncode, uri.Query)
	return uri.WithPath(newPath).WithQuery(newQuery).String()
}

// notFoundTemplateHandler ports NotFoundTemplateHandler.
func (a *App) notFoundTemplateHandler(r *Request) (*Response, error) {
	template := notFoundTemplate
	if r.NotFoundType.IsInvalidShortURL() {
		template = invalidShortCodeTemplate
	}
	return HTMLResponse(template, http.StatusNotFound), nil
}
