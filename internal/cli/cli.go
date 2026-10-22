// Package cli ports Shlink's console commands on top of the console package: every command upstream registers, with
// its exact name, description, help and input definition, and the bodies that run them.
package cli

import (
	"context"
	"fmt"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/integration/geolocation"
	"github.com/NamTranQuoc/shlink-go/internal/integration/matomo"
)

// Services are the dependencies command bodies use.
type Services struct {
	DB          *db.DB
	Store       *core.Store
	URLs        config.URLShortenerOptions
	ShortURLs   *core.ShortURLService
	Tags        *core.TagService
	Domains     *core.DomainService
	APIKeys     *core.APIKeyService
	Visits      *core.VisitsStatsHelper
	Tracker     *core.VisitsTracker
	Transformer core.ShortURLDataTransformer
	// Locks are the file locks shared with the server (data/locks), like upstream's LockFactory.
	Locks           *geolocation.Locks
	VisitLocator    *geolocation.VisitLocator
	VisitToLocation *geolocation.VisitToLocation
	GeoDbUpdater    *geolocation.GeolocationDbUpdater
	MatomoEnabled   bool
	MatomoSender    *matomo.VisitSender
}

// Env gives command bodies their services. Services are built the first time a body asks for them, like upstream's
// ContainerCommandLoader only builds the command (and its dependencies) that runs, so listing commands or rendering
// help never opens a database connection.
type Env struct {
	Ctx      context.Context
	build    func() (*Services, error)
	services *Services
}

// Services returns the lazily built services.
func (e *Env) Services() (*Services, error) {
	if e.services == nil {
		s, err := e.build()
		if err != nil {
			return nil, err
		}
		e.services = s
	}
	return e.services, nil
}

// Body implements a command. Interact runs before input validation when the input is interactive; Execute runs the
// command. Both get a fresh SymfonyStyle from io.Style(), like each phase of an invokable command does upstream.
type Body struct {
	Interact func(env *Env, io *console.IO) error
	Execute  func(env *Env, io *console.IO) (int, error)
}

var bodies = map[string]Body{}

// register attaches a body to the command with the given name. Called from init() functions.
func register(name string, b Body) {
	if _, exists := bodies[name]; exists {
		panic(fmt.Sprintf("command %s registered twice", name))
	}
	bodies[name] = b
}

// NewApplication builds the console application with every Shlink command. build is called at most once, when a
// command body first needs services.
func NewApplication(build func() (*Services, error)) *console.Application {
	app := console.NewApplication("Shlink", config.Version)
	env := &Env{Ctx: context.Background(), build: build}
	for _, c := range commandDefinitions() {
		b, ok := bodies[c.Name]
		if !ok {
			b = notImplemented(c.Name)
		}
		if b.Interact != nil {
			interact := b.Interact
			c.Interact = func(io *console.IO) error { return phpException(interact(env, io)) }
		}
		execute := b.Execute
		c.Execute = func(io *console.IO) (int, error) {
			code, err := execute(env, io)
			return code, phpException(err)
		}
		app.Add(c)
	}
	return app
}

func notImplemented(name string) Body {
	return Body{Execute: func(*Env, *console.IO) (int, error) {
		return 0, fmt.Errorf(`command "%s" is not implemented yet`, name)
	}}
}
