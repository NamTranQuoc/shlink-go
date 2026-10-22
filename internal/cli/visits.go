package cli

import (
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

func init() {
	register("visit:orphan", Body{Execute: orphanVisits})
	register("visit:non-orphan", Body{Execute: nonOrphanVisits})
	register("visit:orphan-delete", Body{Execute: deleteOrphanVisits})
}

// Visits list formats, like VisitsListFormat.
const (
	visitsFormatFull      = "full"
	visitsFormatPaginated = "paginated"
	visitsFormatCSV       = "csv"
)

// visitsInput ports VisitsListInput.
type visitsInput struct {
	startDate, endDate *string
	format             string
}

// visitsListInput resolves the #[MapInput] VisitsListInput properties, in declaration order.
func visitsListInput(in *console.Input) (visitsInput, error) {
	format, err := enumOption(in, "format", []string{visitsFormatFull, visitsFormatPaginated, visitsFormatCSV})
	if err != nil {
		return visitsInput{}, err
	}
	v := visitsInput{startDate: in.OptionString("start-date"), endDate: in.OptionString("end-date"), format: visitsFormatFull}
	if format != nil {
		v.format = *format
	}
	return v, nil
}

// params builds the VisitsParams for the input's date range, with every item in a single page.
func (v visitsInput) params() (core.VisitsParams, error) {
	r, err := dateRange(v.startDate, v.endDate)
	if err != nil {
		return core.VisitsParams{}, err
	}
	return core.VisitsParams{DateRange: r, Page: 1, ItemsPerPage: core.AllItems}, nil
}

var visitsHeaders = []any{"Date", "Potential bot", "User agent", "Referer", "Country", "Region", "City", "Visited URL",
	"Redirect URL", "Type"}

// renderVisits ports VisitsCommandUtils::renderOutput().
func renderVisits(env *Env, out console.Output, input visitsInput, paginator *core.Paginator[*core.Visit]) error {
	if input.format != visitsFormatFull {
		// Avoid running out of memory by loading visits in chunks
		paginator.SetMaxPerPage(1000)
	}
	page := 1
	for {
		paginator.SetCurrentPage(page)
		page++
		visits, err := paginator.Results(env.Ctx)
		if err != nil {
			return err
		}
		rows := make([][]any, len(visits))
		for i, v := range visits {
			rows[i] = visitRow(v)
		}
		if input.format == visitsFormatCSV {
			var b strings.Builder
			if page == 2 {
				writeCSVRecord(&b, visitsHeaders)
			}
			for _, r := range rows {
				writeCSVRecord(&b, r)
			}
			out.Write(b.String(), false, console.OutputNormal)
		} else {
			pages, err := paginator.NbPages(env.Ctx)
			if err != nil {
				return err
			}
			footer := "Page " + phpString(paginator.CurrentPage()) + " of " + phpString(pages)
			shlinkTable(out, false, visitsHeaders, rows, footer, "")
		}
		next, err := paginator.HasNextPage(env.Ctx)
		if err != nil {
			return err
		}
		if !next {
			return nil
		}
	}
}

func visitRow(v *core.Visit) []any {
	country, region, city := "Unknown", "Unknown", "Unknown"
	if l := v.Location; l != nil {
		country, region, city = l.CountryName, l.RegionName, l.CityName
	}
	bot := ""
	if v.PotentialBot {
		bot = "Potential bot"
	}
	return []any{phpx.AtomString(v.Date), bot, v.UserAgent, v.Referer, country, region, city,
		deref(v.VisitedURL, "Unknown"), deref(v.RedirectURL, "Unknown"), string(v.Type)}
}

// writeCSVRecord ports PHP's fputcsv() with league/csv's defaults: "," delimiter, '"' enclosure, "\" escape and "\n"
// line endings.
func writeCSVRecord(b *strings.Builder, fields []any) {
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		s := phpString(f)
		if !strings.ContainsAny(s, ",\"\\\n\r\t ") {
			b.WriteString(s)
			continue
		}
		b.WriteByte('"')
		escaped := false
		for j := 0; j < len(s); j++ {
			c := s[j]
			switch {
			case c == '\\':
				escaped = true
			case !escaped && c == '"':
				b.WriteByte('"')
			default:
				escaped = false
			}
			b.WriteByte(c)
		}
		b.WriteByte('"')
	}
	b.WriteByte('\n')
}

func orphanVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	input, err := visitsListInput(io.Input)
	if err != nil {
		return 0, err
	}
	types := make([]string, len(core.OrphanVisitTypes))
	for i, t := range core.OrphanVisitTypes {
		types[i] = string(t)
	}
	typ, err := enumOption(io.Input, "type", types)
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	params, err := input.params()
	if err != nil {
		return 0, err
	}
	params.Domain = io.Input.OptionString("domain")
	if typ != nil {
		t := core.VisitType(*typ)
		params.OrphanType = &t
	}
	paginator, err := s.Visits.OrphanVisits(env.Ctx, params, nil)
	if err != nil {
		return 0, err
	}
	return console.Success, renderVisits(env, style, input, paginator)
}

func nonOrphanVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	input, err := visitsListInput(io.Input)
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	params, err := input.params()
	if err != nil {
		return 0, err
	}
	params.Domain = io.Input.OptionString("domain")
	paginator, err := s.Visits.NonOrphanVisits(env.Ctx, params, nil)
	if err != nil {
		return 0, err
	}
	return console.Success, renderVisits(env, style, input, paginator)
}

func deleteOrphanVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	return executeWithWarning("You are about to delete all orphan visits. This operation cannot be undone", style, func() (int, error) {
		s, err := env.Services()
		if err != nil {
			return 0, err
		}
		n, err := s.Visits.DeleteOrphanVisits(env.Ctx, nil)
		if err != nil {
			return 0, err
		}
		style.Success("Successfully deleted " + phpString(n) + " visits")
		return console.Success, nil
	})
}
