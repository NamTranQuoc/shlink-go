package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/integration"
	"github.com/NamTranQuoc/shlink-go/internal/web"
)

//go:embed public
var publicFiles embed.FS

// container holds the services shared by the HTTP server and the CLI, like upstream's service container.
type container struct {
	DB        *db.DB
	Store     *core.Store
	URLs      config.URLShortenerOptions
	Tracking  config.TrackingOptions
	ShortURLs *core.ShortURLService
	Tags      *core.TagService
	Domains   *core.DomainService
	APIKeys   *core.APIKeyService
	Visits    *core.VisitsStatsHelper
	Tracker   *core.VisitsTracker
	Logger    *slog.Logger
	// Transformer ports ShortUrlDataTransformer.
	Transformer core.ShortURLDataTransformer
	// Integrations holds Mercure, RabbitMQ, Redis, Matomo, GeoLite2 and title resolution, plus the event dispatcher.
	Integrations *integration.Integrations
}

// newContainer builds the services for CLI commands. Like upstream outside RoadRunner, async event listeners are not
// registered.
func newContainer() (*container, error) {
	return buildContainer(false)
}

// buildContainer builds the services; async registers upstream's async event listeners on background workers, as
// the HTTP server does.
func buildContainer(async bool) (*container, error) {
	d, err := db.Open(db.ParamsFromEnv(), true)
	if err != nil {
		return nil, err
	}
	store := core.NewStore(d)
	urls := config.URLShortenerOptionsFromEnv()
	tracking := config.TrackingOptionsFromEnv()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	transformer := core.ShortURLDataTransformer{Stringifier: core.ShortURLStringifier{
		Options: urls, BasePath: config.BasePath.Load().AsString()}}
	integrations, err := integration.New(integration.Params{
		Store: store, Transformer: transformer, Tracking: tracking, Logger: logger, Async: async,
	})
	if err != nil {
		return nil, err
	}
	return &container{
		DB:       d,
		Store:    store,
		URLs:     urls,
		Tracking: tracking,
		ShortURLs: &core.ShortURLService{Store: store, Options: urls, DeleteOpts: config.DeleteShortURLsOptionsFromEnv(),
			Titles: integrations.Titles, Events: integrations.Events},
		Tags: &core.TagService{Store: store},
		Domains: &core.DomainService{Store: store, DefaultDomain: urls.DefaultDomain,
			NotFoundRedirects: config.NotFoundRedirectOptionsFromEnv()},
		APIKeys:      &core.APIKeyService{Store: store},
		Visits:       &core.VisitsStatsHelper{Store: store, DefaultDomain: urls.DefaultDomain},
		Tracker:      &core.VisitsTracker{Store: store, Options: tracking, Events: integrations.Events},
		Logger:       logger,
		Transformer:  transformer,
		Integrations: integrations,
	}, nil
}

func (c *container) newApp() (*web.App, error) {
	public, err := fs.Sub(publicFiles, "public")
	if err != nil {
		return nil, err
	}
	trusted := config.TrustedProxies.Load()
	var proxies []string
	hops := 0
	if config.IsNumericValue(trusted) {
		hops = trusted.AsInt()
	} else {
		proxies = config.SplitByComma(trusted)
	}
	basePath := config.BasePath.Load().AsString()
	app := &web.App{
		Config: web.Config{
			BasePath:          basePath,
			URLShortener:      c.URLs,
			Tracking:          c.Tracking,
			Redirect:          config.RedirectOptionsFromEnv(),
			NotFoundRedirects: config.NotFoundRedirectOptionsFromEnv(),
			Robots:            config.RobotsOptionsFromEnv(),
			Cors:              config.CorsOptionsFromEnv(),
			TrustedProxies:    proxies,
			TrustedHopCount:   hops,
		},
		Store:       c.Store,
		ShortURLs:   c.ShortURLs,
		Tags:        c.Tags,
		Domains:     c.Domains,
		APIKeys:     c.APIKeys,
		Visits:      c.Visits,
		Tracker:     c.Tracker,
		Transformer: c.Transformer,
		Geo:         c.Integrations.GeoResolver,
		Mercure:     c.Integrations.Mercure,
		Logger:      c.Logger,
		Public:      http.FS(public),
	}
	return app, app.Init()
}

// serve runs the HTTP server until a termination signal arrives, like RoadRunner does upstream. The database is
// initialized beforehand by `shlink-installer init`, which the image entrypoint runs.
func serve() error {
	c, err := buildContainer(true)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := c.newApp()
	if err != nil {
		return err
	}
	addr := envOr("ADDRESS", "0.0.0.0") + ":" + envOr("PORT", "8080")
	srv := &http.Server{Addr: addr, Handler: app, ReadHeaderTimeout: 30 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		// Pending async event listeners finish before exiting, like RoadRunner draining its jobs pipeline
		c.Integrations.Shutdown(shutdown)
	}()
	c.Logger.Info("Shlink listening on " + addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
