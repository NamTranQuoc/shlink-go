// Package web ports Shlink's HTTP layer: the Mezzio middleware pipeline, FastRoute routing, the REST API actions and
// the public redirect/tracking actions, served the way RoadRunner serves the PHP worker.
package web

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// GeoResolver ports the GeoLite2 IP location resolver plus DbUpdater::databaseFileExists().
type GeoResolver interface {
	DatabaseFileExists() bool
	ResolveIPLocation(ip string) (*core.Location, error)
}

// MercureTokens builds subscription tokens for the mercure-info endpoint.
type MercureTokens interface {
	// PublicHubURL returns the configured public hub URL, or nil when Mercure is not configured.
	PublicHubURL() *string
	JWTDays() int
	BuildSubscriptionToken(expiresAt time.Time) (string, error)
}

// Config groups the options the HTTP layer reads.
type Config struct {
	BasePath          string
	URLShortener      config.URLShortenerOptions
	Tracking          config.TrackingOptions
	Redirect          config.RedirectOptions
	NotFoundRedirects config.NotFoundRedirectOptions
	Robots            config.RobotsOptions
	Cors              config.CorsOptions
	TrustedProxies    []string
	TrustedHopCount   int
}

// App wires services into the HTTP pipeline.
type App struct {
	Config      Config
	Store       *core.Store
	ShortURLs   *core.ShortURLService
	Tags        *core.TagService
	Domains     *core.DomainService
	APIKeys     *core.APIKeyService
	Visits      *core.VisitsStatsHelper
	Tracker     *core.VisitsTracker
	Transformer core.ShortURLDataTransformer
	IP          *IPResolver
	Geo         GeoResolver
	Mercure     MercureTokens
	Logger      *slog.Logger
	// Public serves files the way RoadRunner's static middleware serves the public directory.
	Public http.FileSystem

	router  *Router
	handler Handler
}

// Init builds the router and the middleware pipeline. It must be called once before serving.
func (a *App) Init() error {
	if a.Logger == nil {
		a.Logger = slog.Default()
	}
	if a.IP == nil {
		a.IP = NewIPResolver(a.Config.TrustedProxies, a.Config.TrustedHopCount)
	}
	a.router = NewRouter()
	for _, route := range a.routes() {
		if err := a.router.AddRoute(route); err != nil {
			return err
		}
	}
	a.handler = a.pipeline()
	return nil
}

// pathMiddleware ports Stratigility's PathMiddlewareDecorator: the middleware only runs for requests under prefix
// (case-insensitively, at a path border) and sees the URI with the prefix stripped. Next handlers get the configured
// prefix prepended back, which normalizes its casing.
func pathMiddleware(prefix string, mw Middleware) Middleware {
	if len(prefix) > 1 {
		prefix = strings.TrimRight(prefix, "/")
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return func(r *Request, next Handler) (*Response, error) {
		path := r.URI.Path
		if path == "" {
			path = "/"
		}
		if len(path) < len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
			return next(r)
		}
		if prefix != "/" && len(path) > len(prefix) && path[len(prefix)] != '/' {
			return next(r)
		}
		stripped := r
		if prefix != "/" {
			stripped = r.Clone()
			stripped.URI = r.URI.WithPath(r.URI.Path[min(len(prefix), len(r.URI.Path)):])
		}
		return mw(stripped, func(inner *Request) (*Response, error) {
			restored := inner.Clone()
			restored.URI = inner.URI.WithPath(prefix + inner.URI.Path)
			return next(restored)
		})
	}
}

func (a *App) pipeline() Handler {
	rest := a.Config.BasePath + "/rest"
	return Chain(a.notFoundTemplateHandler,
		a.accessLogMiddleware,
		contentLengthMiddleware,
		requestIDMiddleware,
		a.errorHandlerMiddleware,
		a.crossDomainMiddleware,
		pathMiddleware(rest, a.problemDetailsMiddleware),
		a.routeMiddleware,
		a.implicitHeadMiddleware,
		pathMiddleware(rest, implicitOptionsMiddleware),
		pathMiddleware(rest, bodyParserMiddleware),
		pathMiddleware(rest, a.authenticationMiddleware),
		dispatchMiddleware,
		pathMiddleware(rest, problemDetailsNotFoundHandler),
		a.ipAddressMiddleware,
		a.ipGeolocationMiddleware,
		a.notFoundTypeResolverMiddleware,
		a.extraPathRedirectMiddleware,
		a.notFoundTrackerMiddleware,
		a.notFoundRedirectHandler,
	)
}

func (a *App) accessLogMiddleware(r *Request, next Handler) (*Response, error) {
	resp, err := next(r)
	if err != nil {
		return nil, err
	}
	uri := r.URI.Path
	if r.URI.Query != "" {
		uri += "?" + r.URI.Query
	}
	if !strings.HasSuffix(r.URI.Path, "/rest/health") {
		a.Logger.Info(fmt.Sprintf("%s %s %d %s", r.Method, uri, resp.Status, resp.Header.Get("Content-Length")))
	}
	return resp, nil
}

// contentLengthMiddleware ports ContentLengthMiddleware.
func contentLengthMiddleware(r *Request, next Handler) (*Response, error) {
	resp, err := next(r)
	if err != nil {
		return nil, err
	}
	if len(resp.Header.Values("Content-Length")) == 0 && !resp.Streamed {
		resp.Header.Set("Content-Length", fmt.Sprint(len(resp.Body)))
	}
	return resp, nil
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// requestIDMiddleware ports RequestIdMiddleware.
func requestIDMiddleware(r *Request, next Handler) (*Response, error) {
	r = r.Clone()
	if r.HasHeader("X-Request-Id") {
		r.RequestID = r.Header("X-Request-Id")
	} else {
		r.RequestID = uuid4()
	}
	resp, err := next(r)
	if err != nil {
		return nil, err
	}
	resp.Header.Set("X-Request-Id", r.RequestID)
	return resp, nil
}

// errorHandlerMiddleware ports Laminas' ErrorHandler with Mezzio's default ErrorResponseGenerator (no templates, no
// debug mode).
func (a *App) errorHandlerMiddleware(r *Request, next Handler) (resp *Response, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			a.Logger.Error(fmt.Sprintf("%v", rec))
			resp, err = NewResponse(500), nil
			resp.Body = []byte("An unexpected error occurred")
		}
	}()
	resp, err = next(r)
	if err != nil {
		a.Logger.Error(err.Error())
		resp = NewResponse(500)
		resp.Body = []byte("An unexpected error occurred")
		return resp, nil
	}
	return resp, nil
}

// crossDomainMiddleware ports CrossDomainMiddleware.
func (a *App) crossDomainMiddleware(r *Request, next Handler) (*Response, error) {
	resp, err := next(r)
	if err != nil || !r.HasHeader("Origin") {
		return resp, err
	}
	if origin := a.Config.Cors.AllowOriginFor(r.Header("Origin")); origin != "" {
		resp.Header.Set("Access-Control-Allow-Origin", origin)
	}
	if a.Config.Cors.AllowCredentials {
		resp.Header.Set("Access-Control-Allow-Credentials", "true")
	}
	if r.Method != http.MethodOptions {
		return resp, nil
	}
	allowed := strings.Join(resp.Header.Values("Allow"), ", ")
	if len(resp.Header.Values("Allow")) == 0 {
		allowed = "GET,POST,PUT,PATCH,DELETE"
	}
	empty := EmptyResponse()
	for k, v := range resp.Header {
		empty.Header[k] = v
	}
	empty.Header.Set("Access-Control-Allow-Methods", allowed)
	empty.Header.Set("Access-Control-Allow-Headers", r.Header("Access-Control-Request-Headers"))
	empty.Header.Set("Access-Control-Max-Age", fmt.Sprint(a.Config.Cors.MaxAge))
	return empty, nil
}

// problemDetailsMiddleware ports ProblemDetailsMiddleware with the ErrorLogger listener attached.
func (a *App) problemDetailsMiddleware(r *Request, next Handler) (*Response, error) {
	if problemMediaType(r.Header("Accept")) == "" {
		return next(r)
	}
	resp, err := next(r)
	if err != nil {
		resp = problemResponseFromError(r, err)
		level := slog.LevelDebug
		if resp.Status >= 500 {
			level = slog.LevelError
		}
		a.Logger.Log(r.HTTP.Context(), level, err.Error())
		return resp, nil
	}
	return resp, nil
}

// problemDetailsNotFoundHandler ports ProblemDetailsNotFoundHandler.
func problemDetailsNotFoundHandler(r *Request, next Handler) (*Response, error) {
	if problemMediaType(r.Header("Accept")) == "" {
		return next(r)
	}
	return problemResponse(r, 404, fmt.Sprintf("Cannot %s %s!", r.Method, r.URI.String()), "", "", nil), nil
}

// routeMiddleware ports Mezzio's RouteMiddleware.
func (a *App) routeMiddleware(r *Request, next Handler) (*Response, error) {
	r = r.Clone()
	r.Route = a.router.Match(r.Method, r.URI)
	if !r.Route.Failure {
		r.Params = r.Route.Params
	}
	return next(r)
}

// implicitHeadMiddleware ports Mezzio's ImplicitHeadMiddleware.
func (a *App) implicitHeadMiddleware(r *Request, next Handler) (*Response, error) {
	if r.Method != http.MethodHead || r.Route == nil || r.Route.Route != nil {
		return next(r)
	}
	result := a.router.Match(http.MethodGet, r.URI)
	if result.Failure {
		return next(r)
	}
	r = r.Clone()
	r.Params = result.Params
	r.Route = result
	r.ForwardedHead = true
	r.Method = http.MethodGet
	resp, err := next(r)
	if err != nil {
		return nil, err
	}
	resp.Body = nil
	return resp, nil
}

// implicitOptionsMiddleware ports ImplicitOptionsMiddleware with Shlink's empty-response factory.
func implicitOptionsMiddleware(r *Request, next Handler) (*Response, error) {
	if r.Method != http.MethodOptions || r.Route == nil {
		return next(r)
	}
	if r.Route.Failure && !r.Route.IsMethodFailure() {
		return next(r)
	}
	if r.Route.Route != nil {
		return next(r)
	}
	resp := EmptyResponse()
	resp.Header.Set("Allow", strings.Join(r.Route.AllowedMethods, ","))
	return resp, nil
}

// dispatchMiddleware ports Mezzio's DispatchMiddleware: matched routes run their own middleware pipe, which delegates
// to the rest of the application pipeline when it does not produce a response.
func dispatchMiddleware(r *Request, next Handler) (*Response, error) {
	if r.Route == nil || r.Route.Route == nil {
		return next(r)
	}
	return r.Route.Route.Handler(r, next)
}

// maxRequestSize is RoadRunner's default http.max_request_size (1000 MB), which upstream does not override.
const maxRequestSize = 1000 << 20

// ServeHTTP adapts net/http to the pipeline, the way RoadRunner hands requests to the PHP worker.
func (a *App) ServeHTTP(w http.ResponseWriter, hr *http.Request) {
	if a.serveStatic(w, hr) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, hr.Body, maxRequestSize))
	if maxErr := (*http.MaxBytesError)(nil); errors.As(err, &maxErr) {
		http.Error(w, "serve_http: http: request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	remote, _, err := net.SplitHostPort(hr.RemoteAddr)
	if err != nil {
		remote = hr.RemoteAddr
	}
	scheme := "http"
	if hr.TLS != nil {
		scheme = "https"
	}
	uri, _ := phpx.ParseURI(scheme + "://" + hr.Host + hr.URL.RequestURI())
	req := &Request{
		HTTP:       hr,
		Method:     hr.Method,
		URI:        uri,
		Query:      ParseStr(hr.URL.RawQuery),
		RawBody:    body,
		RemoteAddr: remote,
	}
	if parsed, ok := parseFormBody(hr, body); ok {
		req.Body = parsed
	}
	resp, err := a.handler(req)
	if err != nil || resp == nil {
		resp = NewResponse(500)
		resp.Body = []byte("An unexpected error occurred")
	}
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.Status)
	if resp.Streamed {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if len(resp.Body) > 0 {
		_, _ = io.Copy(w, bytes.NewReader(resp.Body))
	}
}
