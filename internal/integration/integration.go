// Package integration wires Shlink's optional integrations (real-time updates through Mercure, RabbitMQ and Redis,
// Matomo, GeoLite2 geolocation and title resolution) and the event listeners that drive them, following
// module/Core/config/event_dispatcher.config.php.
package integration

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/integration/events"
	"github.com/NamTranQuoc/shlink-go/internal/integration/geolocation"
	"github.com/NamTranQuoc/shlink-go/internal/integration/matomo"
	"github.com/NamTranQuoc/shlink-go/internal/integration/realtime"
	"github.com/NamTranQuoc/shlink-go/internal/integration/title"
)

// Integrations holds the integration services shared by the HTTP server and the CLI.
type Integrations struct {
	Events          *events.Dispatcher
	Mercure         *realtime.Mercure
	MatomoOptions   matomo.Options
	MatomoSender    *matomo.VisitSender
	GeoLiteOptions  geolocation.Options
	DbUpdater       *geolocation.DbUpdater
	GeoResolver     *geolocation.Resolver
	GeoDbUpdater    *geolocation.GeolocationDbUpdater
	VisitLocator    *geolocation.VisitLocator
	VisitToLocation *geolocation.VisitToLocation
	Titles          *title.Resolver
	Logger          *slog.Logger

	updateGeoLiteDb *geolocation.UpdateGeoLiteDb
}

// Params are the core services integrations need.
type Params struct {
	Store       *core.Store
	Transformer core.ShortURLDataTransformer
	Tracking    config.TrackingOptions
	Logger      *slog.Logger
	// Async registers upstream's async listeners on background workers, like the RoadRunner server does. Without it
	// (CLI processes, where RR_MODE is not set) async listeners are not registered at all.
	Async bool
}

// New builds the integrations from env vars.
func New(p Params) (*Integrations, error) {
	i := &Integrations{
		Mercure:        realtime.NewMercure(realtime.MercureOptionsFromEnv()),
		MatomoOptions:  matomo.OptionsFromEnv(),
		GeoLiteOptions: geolocation.OptionsFromEnv(),
		Titles:         title.NewResolver(p.Logger),
		Logger:         p.Logger,
	}
	i.MatomoSender = matomo.NewVisitSender(i.MatomoOptions, p.Store, p.Transformer.Stringifier)
	i.DbUpdater = geolocation.NewDbUpdater(i.GeoLiteOptions)
	i.GeoResolver = &geolocation.Resolver{Updater: i.DbUpdater}
	i.GeoDbUpdater = &geolocation.GeolocationDbUpdater{
		Updater: i.DbUpdater, Store: p.Store, Tracking: p.Tracking, Locks: &geolocation.Locks{Dir: "data/locks"},
	}
	i.VisitLocator = &geolocation.VisitLocator{Store: p.Store}
	i.VisitToLocation = &geolocation.VisitToLocation{Resolver: i.GeoResolver}

	var listeners events.Listeners
	matomoListener := &matomo.Listener{Sender: i.MatomoSender, Logger: p.Logger}
	locate := &geolocation.LocateUnlocatedVisits{Locator: i.VisitLocator, VisitToLocation: i.VisitToLocation}
	listeners.GeoLiteDbCreated = append(listeners.GeoLiteDbCreated, func(ctx context.Context) {
		if err := locate.Handle(ctx); err != nil {
			p.Logger.Error(err.Error())
		}
	})
	workers := -1
	if p.Async {
		workers = 0
		if n, err := strconv.Atoi(os.Getenv("TASK_WORKER_NUM")); err == nil && n > 0 {
			workers = n
		}
		if err := i.registerAsyncListeners(p, &listeners); err != nil {
			return nil, err
		}
		// SendVisitToMatomo is async in RoadRunner, registered only when Matomo is enabled
		if i.MatomoOptions.Enabled {
			listeners.URLVisited = append(listeners.URLVisited, matomoListener.Handle)
		}
	} else {
		// Outside RoadRunner SendVisitToMatomo is a regular listener, so it is always registered
		listeners.URLVisited = append(listeners.URLVisited, matomoListener.Handle)
	}
	i.Events = events.NewDispatcher(listeners, workers, p.Logger)
	if i.updateGeoLiteDb != nil {
		i.updateGeoLiteDb.Events = i.Events
	}
	return i, nil
}

// registerAsyncListeners ports the async listeners of event_dispatcher.config.php, filtered by
// EnabledListenerChecker. Their order matches the config.
func (i *Integrations) registerAsyncListeners(p Params, l *events.Listeners) error {
	topics, err := config.RealTimeUpdatesOptionsFromEnv()
	if err != nil {
		return err
	}
	generator := &realtime.Generator{Store: p.Store, Transformer: p.Transformer}
	notifier := func(system string, pub realtime.Publisher) *realtime.Notifier {
		return &realtime.Notifier{System: system, Publisher: pub, Generator: generator, Store: p.Store, Topics: topics,
			Logger: p.Logger}
	}
	var notifiers []*realtime.Notifier
	if i.Mercure.Options.Enabled {
		notifiers = append(notifiers, notifier(realtime.SystemMercure, i.Mercure))
	}
	rabbit := realtime.RabbitMQOptionsFromEnv()
	var rabbitNotifier *realtime.Notifier
	if rabbit.Enabled {
		rabbitNotifier = notifier(realtime.SystemRabbitMQ, &realtime.RabbitMQ{Options: rabbit})
	}
	redisOptions := realtime.RedisOptionsFromEnv()
	var redisNotifier *realtime.Notifier
	if redisOptions.PubSubEnabled {
		client, err := realtime.NewRedisClient(redisOptions)
		if err != nil {
			return err
		}
		redisNotifier = notifier(realtime.SystemRedisPubSub, &realtime.Redis{Client: client})
	}
	for _, n := range []*realtime.Notifier{rabbitNotifier, redisNotifier} {
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	for _, n := range notifiers {
		l.URLVisited = append(l.URLVisited, func(ctx context.Context, visitID int64, _ *string) { n.NotifyVisit(ctx, visitID) })
	}
	if i.GeoLiteOptions.HasLicenseKey() {
		i.updateGeoLiteDb = &geolocation.UpdateGeoLiteDb{Updater: i.GeoDbUpdater, Logger: p.Logger}
		l.URLVisited = append(l.URLVisited, i.updateGeoLiteDb.Handle)
	}
	for _, n := range notifiers {
		l.ShortURLCreated = append(l.ShortURLCreated, n.NotifyNewShortURL)
	}
	return nil
}

// Shutdown waits for pending async listeners.
func (i *Integrations) Shutdown(ctx context.Context) {
	i.Events.Shutdown(ctx)
}
