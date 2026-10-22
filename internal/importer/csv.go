package importer

import (
	"bufio"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// csvImporter ports CsvImporter, reading the file like league/csv's Reader with a header offset of 0.
type csvImporter struct {
	now func() time.Time
}

// ErrCSVEmptyPath ports the ValueError fopen() throws when the CSV importer gets no stream (non-interactive runs).
var ErrCSVEmptyPath = errors.New("Path must not be empty")

// CSVSyntaxError ports league/csv's SyntaxError, thrown while reading the header.
type CSVSyntaxError struct {
	Message string
	Line    int
}

func (e *CSVSyntaxError) Error() string { return e.Message }

// CSVMissingLongURLError ports the TypeError ImportedShlinkUrl's constructor throws when a record has no long URL.
// KeyMissing tells if the column does not exist at all (PHP then warns about the undefined key first).
type CSVMissingLongURLError struct {
	KeyMissing bool
	Domain     *string
	ShortCode  string
	Title      *string
}

func (e *CSVMissingLongURLError) Error() string { return "missing long URL" }

func (i *csvImporter) Import(p Params) (Result, error) {
	stream, _ := p.Extra["stream"].(io.Reader)
	delimiter := deref(p.extraString("delimiter"))
	return Result{ShortURLs: func(yield func(*ShortURL, error) bool) {
		if stream == nil {
			yield(nil, ErrCSVEmptyPath)
			return
		}
		now := i.now()
		r := phpx.NewCSVReader(skipBOM(stream))
		if len(delimiter) == 1 {
			r.Delimiter = delimiter[0]
		}
		header, err := r.Read()
		if err != nil || (len(header) == 1 && header[0] == nil) {
			yield(nil, &CSVSyntaxError{Message: "The header record does not exist or is empty at offset: `0`", Line: 45})
			return
		}
		seen := map[string]bool{}
		for _, h := range header {
			if seen[*h] {
				yield(nil, &CSVSyntaxError{Message: "The header record contains duplicate column names.", Line: 55})
				return
			}
			seen[*h] = true
		}
		for {
			fields, err := r.Read()
			if errors.Is(err, io.EOF) {
				return
			}
			if len(fields) == 1 && fields[0] == nil {
				// league/csv skips empty records
				continue
			}
			record := remapRecordHeaders(header, fields)
			shortCode, domain := parseShortCodeAndDomain(record)
			title := nonEmptyValueOrNull(record, "title")
			longURL, hasLongURL := record["longurl"]
			if longURL == nil {
				yield(nil, &CSVMissingLongURLError{KeyMissing: !hasLongURL, Domain: domain, ShortCode: shortCode, Title: title})
				return
			}
			su := &ShortURL{
				Source:    CSV,
				LongURL:   *longURL,
				Tags:      parseTags(record),
				CreatedAt: now,
				Domain:    domain,
				ShortCode: shortCode,
				Title:     title,
				Visits:    noVisits,
			}
			if !yield(su, nil) {
				return
			}
		}
	}, OrphanVisits: noOrphanVisits}, nil
}

func skipBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && string(b) == "\xef\xbb\xbf" {
		_, _ = br.Discard(3)
	}
	return br
}

// remapRecordHeaders combines a record with the header (missing fields are null, extra ones are dropped), with keys
// lower-cased and without spaces.
func remapRecordHeaders(header, fields []*string) map[string]*string {
	record := make(map[string]*string, len(header))
	for i, h := range header {
		var v *string
		if i < len(fields) {
			v = fields[i]
		}
		record[strings.ToLower(strings.ReplaceAll(*h, " ", ""))] = v
	}
	return record
}

func nonEmptyValueOrNull(record map[string]*string, key string) *string {
	v := ""
	if p := record[key]; p != nil {
		v = *p
	}
	v = strings.Trim(v, " \t\n\r\x00\x0B")
	if v == "" || v == "0" {
		return nil
	}
	return &v
}

func parseTags(record map[string]*string) []string {
	raw := deref(nonEmptyValueOrNull(record, "tags"))
	separator := "|"
	if strings.Contains(raw, ",") {
		separator = ","
	}
	var tags []string
	for _, t := range strings.Split(raw, separator) {
		if t != "" && t != "0" {
			tags = append(tags, t)
		}
	}
	return tags
}

func parseShortCodeAndDomain(record map[string]*string) (string, *string) {
	parsed, _ := url.Parse(deref(record["shorturl"]))
	shortCode := ""
	if sc := record["shortcode"]; sc != nil {
		shortCode = *sc
	} else if parsed != nil && len(parsed.Path) > 0 {
		shortCode = parsed.Path[1:]
	}
	if _, ok := record["domain"]; ok {
		return shortCode, nonEmptyValueOrNull(record, "domain")
	}
	if parsed != nil && parsed.Host != "" {
		h := parsed.Hostname()
		return shortCode, &h
	}
	return shortCode, nil
}
