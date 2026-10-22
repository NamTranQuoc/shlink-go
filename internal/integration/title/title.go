// Package title ports ShortUrlTitleResolutionHelper: the long URL is fetched with a browser user agent and the
// page's <title> is read, converted to UTF-8 and HTML-decoded.
package title

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/text/encoding/htmlindex"
)

const (
	maxRedirects    = 15
	chromeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36"
	timeout         = 3 * time.Second
	readChunk       = 1024
)

var (
	titleTagValue   = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	charsetValue    = regexp.MustCompile(`(?i)charset=([^;]+)`)
	charsetFromMeta = regexp.MustCompile(`(?i)<meta\b[^>]*\bcharset\s*=\s*["']?([^"'\s>;]+)`)
)

// Resolver implements core.TitleResolver.
type Resolver struct {
	Client *http.Client
	Logger *slog.Logger
}

// NewResolver creates a resolver whose HTTP client applies the upstream request options: 3 second connect and total
// timeouts, at most 15 redirects and IDN conversion.
func NewResolver(logger *slog.Logger) *Resolver {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: timeout}).DialContext
	return &Resolver{
		Logger: logger,
		Client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > maxRedirects {
					return fmt.Errorf("Will not follow more than %d redirects", maxRedirects)
				}
				return nil
			},
		},
	}
}

// fetch ports ShortUrlTitleResolutionHelper::fetchUrl(); any failure, including 4xx/5xx responses (Guzzle's
// http_errors), returns nil.
func (r *Resolver) fetch(ctx context.Context, url string) *http.Response {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if host := req.URL.Hostname(); host != "" {
		if ascii, err := idna.Lookup.ToASCII(host); err == nil && ascii != host {
			if port := req.URL.Port(); port != "" {
				req.URL.Host = net.JoinHostPort(ascii, port)
			} else {
				req.URL.Host = ascii
			}
		}
	}
	req.Header.Set("User-Agent", chromeUserAgent)
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil
	}
	return resp
}

// ResolveTitle ports ShortUrlTitleResolutionHelper::processTitle() for a long URL. It returns nil when no title could
// be resolved.
func (r *Resolver) ResolveTitle(ctx context.Context, longURL string) *string {
	resp := r.fetch(ctx, longURL)
	if resp == nil {
		return nil
	}
	defer resp.Body.Close()
	contentType := strings.ToLower(strings.Join(resp.Header.Values("Content-Type"), ", "))
	if !strings.HasPrefix(contentType, "text/html") {
		return nil
	}
	title := r.tryToResolveTitle(resp.Body, contentType)
	if title == nil {
		return nil
	}
	decoded := html.UnescapeString(phpTrim(*title))
	return &decoded
}

func phpTrim(s string) string { return strings.Trim(s, " \t\n\r\x00\x0B") }

type bodyReader struct {
	body io.Reader
	eof  bool
}

func (b *bodyReader) read() string {
	buf := make([]byte, readChunk)
	n, err := io.ReadFull(b.body, buf)
	if err != nil {
		b.eof = true
	}
	return string(buf[:n])
}

func (r *Resolver) tryToResolveTitle(body io.Reader, contentType string) *string {
	br := &bodyReader{body: body}
	collected := ""
	for !strings.Contains(collected, "</title>") && !br.eof {
		collected += br.read()
	}
	m := titleTagValue.FindStringSubmatch(collected)
	if m == nil {
		return nil
	}
	original := m[1]
	charset := resolvePageCharset(contentType, br, collected)
	if charset == nil {
		return nil
	}
	if original == "" {
		return &original
	}
	if converted, err := toUTF8(original, *charset); err == nil {
		return &converted
	} else if r.Logger != nil {
		r.Logger.Warn(fmt.Sprintf("It was impossible to encode page title in UTF-8 with mb_convert_encoding. %v", err))
	}
	return &original
}

func resolvePageCharset(contentType string, br *bodyReader, collected string) *string {
	if m := charsetValue.FindStringSubmatch(contentType); m != nil {
		return &m[1]
	}
	read := func(s string) *string {
		if m := charsetFromMeta.FindStringSubmatch(s); m != nil {
			return &m[1]
		}
		return nil
	}
	charset := read(collected)
	for charset == nil && !br.eof {
		collected += br.read()
		charset = read(collected)
	}
	return charset
}

// toUTF8 ports mb_convert_encoding($title, 'utf-8', $charset).
func toUTF8(s, charset string) (string, error) {
	enc, err := htmlindex.Get(strings.TrimSpace(charset))
	if err != nil {
		return "", err
	}
	out, err := enc.NewDecoder().String(s)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", errors.New("empty conversion result")
	}
	return out, nil
}
