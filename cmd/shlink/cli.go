package main

import (
	"os"
	"path/filepath"

	"github.com/NamTranQuoc/shlink-go/internal/cli"
	"github.com/NamTranQuoc/shlink-go/internal/config"
)

// run dispatches to the HTTP server, the installer or Shlink's console application. Invoked as `shlink-installer`
// (vendor/bin/shlink-installer in the image), it runs the installer; `serve` starts the server; any other argument
// list runs the console like upstream's bin/cli, and its exit code ends the process.
func run(args []string) error {
	if filepath.Base(os.Args[0]) == "shlink-installer" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		// On macOS os.Executable returns the shlink-installer link itself, which would run the installer again in
		// every child process
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return err
		}
		os.Exit(cli.NewInstallerApplication(exe).Run(args))
	}
	// Promote env vars from the installer's config, like config/container.php
	if err := config.LoadEnvVarsFromParams(); err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "serve" {
		return serve()
	}
	os.Exit(cli.NewApplication(cliServices).Run(args))
	return nil
}

// cliServices builds the services CLI commands use.
func cliServices() (*cli.Services, error) {
	c, err := newContainer()
	if err != nil {
		return nil, err
	}
	return &cli.Services{
		DB:              c.DB,
		Store:           c.Store,
		URLs:            c.URLs,
		ShortURLs:       c.ShortURLs,
		Tags:            c.Tags,
		Domains:         c.Domains,
		APIKeys:         c.APIKeys,
		Visits:          c.Visits,
		Tracker:         c.Tracker,
		Transformer:     c.Transformer,
		Locks:           c.Locks,
		VisitLocator:    c.Integrations.VisitLocator,
		VisitToLocation: c.Integrations.VisitToLocation,
		GeoDbUpdater:    c.Integrations.GeoDbUpdater,
		MatomoEnabled:   c.Integrations.MatomoOptions.Enabled,
		MatomoSender:    c.Integrations.MatomoSender,
	}, nil
}
