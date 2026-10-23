package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/integration/geolocation"
	"github.com/NamTranQuoc/shlink-go/internal/integration/matomo"
)

func init() {
	register("visit:download-db", Body{Execute: downloadGeoLiteDb})
	register("visit:locate", Body{Execute: locateVisits})
	register("integration:matomo:send-visits", Body{Execute: sendVisitsToMatomo})
}

// downloadProgress ports DownloadGeoLiteDbCommand's progress handler.
type downloadProgress struct {
	style *console.SymfonyStyle
	bar   *console.ProgressBar
}

func (p *downloadProgress) BeforeDownload(olderDBExists bool) {
	action := "Downloading"
	if olderDBExists {
		action = "Updating"
	}
	p.style.Text(fmt.Sprintf("<fg=blue>%s GeoLite2 db file...</>", action))
	p.bar = console.NewProgressBar(p.style)
}

func (p *downloadProgress) HandleProgress(total, downloaded int64, _ bool) {
	if p.bar != nil {
		p.bar.SetMaxSteps(int(total))
		p.bar.SetProgress(int(downloaded))
	}
}

func downloadGeoLiteDb(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	progress := &downloadProgress{style: style}
	result, err := s.GeoDbUpdater.CheckDbUpdate(env.Ctx, progress)
	var failed *geolocation.DbUpdateFailedError
	if errors.As(err, &failed) {
		if failed.OlderDBExists {
			style.Warning("GeoLite2 db file update failed. Visits will continue to be located with the old version.")
		} else {
			style.Error("GeoLite2 db file download failed. It will not be possible to locate visits.")
		}
		if style.IsVerbose() {
			io.App.RenderError(err, style)
		}
		if failed.OlderDBExists {
			return console.Invalid, nil
		}
		return console.Failure, nil
	}
	if err != nil {
		return 0, err
	}
	switch result {
	case geolocation.LicenseMissing:
		style.Warning("It was not possible to download GeoLite2 db, because a license was not provided.")
		return console.Invalid, nil
	case geolocation.MaxErrorsReached:
		style.Warning("Max consecutive errors reached. Cannot retry for a couple of days.")
		return console.Invalid, nil
	case geolocation.UpdateInProgress:
		style.Warning("A geolocation db is already being downloaded by another process.")
		return console.Invalid, nil
	}
	if progress.bar == nil {
		style.Info("GeoLite2 db file is up to date.")
	} else {
		progress.bar.Finish()
		style.Success("GeoLite2 db file properly downloaded.")
	}
	return console.Success, nil
}

// locateHelper ports LocateVisitsCommand's VisitGeolocationHelperInterface implementation.
type locateHelper struct {
	io    *console.IO
	style *console.SymfonyStyle
	s     *Services
}

func (h *locateHelper) GeolocateVisit(v *core.Visit) (core.Location, error) {
	h.style.Write(fmt.Sprintf("Processing IP <fg=blue>%s</>", deref(v.RemoteAddr, "?")), false, console.OutputNormal)
	loc, err := h.s.VisitToLocation.ResolveVisitLocation(v)
	var unlocatable *geolocation.IPCannotBeLocatedError
	if errors.As(err, &unlocatable) {
		switch unlocatable.Type {
		case geolocation.EmptyAddress:
			h.style.Writeln(" [<comment>Ignored visit with no IP address</comment>]", console.OutputNormal)
		case geolocation.Localhost:
			h.style.Writeln(" [<comment>Ignored localhost address</comment>]", console.OutputNormal)
		default:
			h.style.Writeln(" [<fg=red>An error occurred while locating IP. Skipped</>]", console.OutputNormal)
			if h.style.IsVerbose() {
				h.io.App.RenderError(err, h.style)
			}
		}
	}
	return loc, err
}

func (h *locateHelper) OnVisitLocated(location core.VisitLocation, v *core.Visit) {
	if !location.IsEmpty {
		h.style.Writeln(fmt.Sprintf(` [<info>Address located in "%s"</info>]`, location.CountryName), console.OutputNormal)
	} else if v.RemoteAddr != nil && *v.RemoteAddr != "" && *v.RemoteAddr != "127.0.0.1" {
		h.style.Writeln(" <comment>[Could not locate address]</comment>", console.OutputNormal)
	}
}

func locateVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	retry, all := io.Input.OptionBool("retry"), io.Input.OptionBool("all")
	if all && !retry {
		style.Writeln("<comment>The <fg=yellow;options=bold>--all</> flag has no effect on its own. You have to provide it "+
			"together with <fg=yellow;options=bold>--retry</>.</comment>", console.OutputNormal)
	}
	if all && retry {
		style.Warning("You are about to process the location of all existing visits your short URLs received.",
			"Since shlink saves visitors IP addresses anonymized, you could end up losing precision on some of your visits.",
			"Also, if you have a large amount of visits, this can be a very time consuming process. Continue at your own risk.")
		proceed, err := style.Confirm("Do you want to proceed?", false)
		if err != nil {
			return 0, err
		}
		if !proceed {
			return 0, console.NewConsoleError(`Symfony\Component\Console\Exception\RuntimeException`, "Execution aborted",
				"LocateVisitsCommand.php", 72)
		}
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	return executeWithLock(env, s.Locks, "visit:locate", false, style, func() (int, error) {
		return locateVisitsWithLock(env, io, style, s, retry, all)
	})
}

func locateVisitsWithLock(env *Env, io *console.IO, style *console.SymfonyStyle, s *Services, retry, all bool) (int, error) {
	err := func() error {
		download, err := io.App.Find("visit:download-db")
		if err != nil {
			return err
		}
		code, err := download.Run(io.With(console.NewArrayInput(map[string]any{}), style))
		if err != nil {
			return err
		}
		if code == console.Failure {
			return errors.New("It is not possible to locate visits without a GeoLite2 db file.")
		}
		h := &locateHelper{io: io, style: style, s: s}
		if all {
			return s.VisitLocator.LocateAllVisits(env.Ctx, h)
		}
		if err := s.VisitLocator.LocateUnlocatedVisits(env.Ctx, h); err != nil {
			return err
		}
		if retry {
			return s.VisitLocator.LocateVisitsWithEmptyLocation(env.Ctx, h)
		}
		return nil
	}()
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) && filepath.Base(pathErr.Path) == "GeoLite2-City.mmdb" {
			// The GeoIP2 reader fails like this when the db file is missing
			err = &console.PHPError{Class: `MaxMind\Db\Reader\InvalidDatabaseException`, File: "Reader.php", Line: 75,
				Message: `The file "/etc/shlink/config/autoload/../../data/GeoLite2-City.mmdb" does not exist or is not readable.`}
		}
		style.Error(err.Error())
		if style.IsVerbose() {
			io.App.RenderError(err, style)
		}
		return console.Failure, nil
	}
	style.Success("Finished locating visits")
	return console.Success, nil
}

// matomoProgress ports MatomoSendVisitsCommand's VisitSendingProgressTrackerInterface implementation.
type matomoProgress struct {
	io    *console.IO
	style *console.SymfonyStyle
}

func (p *matomoProgress) Success(int) { p.style.Write(".", false, console.OutputNormal) }

func (p *matomoProgress) Error(_ int, err error) {
	p.style.Write("<error>E</error>", false, console.OutputNormal)
	if p.style.IsVerbose() {
		p.io.App.RenderError(err, p.style)
	}
}

var _ matomo.ProgressTracker = (*matomoProgress)(nil)

// dateRangeToHumanFriendly ports Shlinkio\Shlink\Core\dateRangeToHumanFriendly().
func dateRangeToHumanFriendly(r core.DateRange) string {
	const layout = "2006-01-02 15:04:05"
	switch {
	case r.Start != nil && r.End != nil:
		return fmt.Sprintf("Between %s and %s", r.Start.Format(layout), r.End.Format(layout))
	case r.Start != nil:
		return "Since " + r.Start.Format(layout)
	case r.End != nil:
		return "Until " + r.End.Format(layout)
	}
	return "All time"
}

func sendVisitsToMatomo(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	if !s.MatomoEnabled {
		style.Warning("Matomo integration is not enabled in this Shlink instance")
		return console.Invalid, nil
	}
	r, err := dateRange(io.Input.OptionString("since"), io.Input.OptionString("until"))
	if err != nil {
		return 0, err
	}
	if io.Input.IsInteractive() {
		style.Warning("You are about to send visits from this Shlink instance to Matomo",
			"Resolved date range -> "+dateRangeToHumanFriendly(r),
			"Shlink will not check for already sent visits, which could result in some duplications. Make sure you have "+
				"verified only visits in the right date range are going to be sent.")
		proceed, err := style.Confirm("Continue?", false)
		if err != nil {
			return 0, err
		}
		if !proceed {
			return console.Invalid, nil
		}
	}
	result, err := s.MatomoSender.SendVisitsInDateRange(env.Ctx, r, &matomoProgress{io: io, style: style})
	if err != nil {
		return 0, err
	}
	switch {
	case result.HasFailures() && result.HasSuccesses():
		style.Warning(fmt.Sprintf("%d visits sent to Matomo. %d failed.", result.SuccessfulVisits, result.FailedVisits))
	case result.HasFailures():
		style.Error(fmt.Sprintf("Failed to send %d visits to Matomo.", result.FailedVisits))
	case result.HasSuccesses():
		style.Success(fmt.Sprintf("%d visits sent to Matomo.", result.SuccessfulVisits))
	default:
		style.Info("There was no visits matching provided date range.")
	}
	return console.Success, nil
}
