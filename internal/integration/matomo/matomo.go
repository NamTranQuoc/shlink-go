// Package matomo ports Shlink's Matomo integration: MatomoOptions, MatomoTrackerBuilder, MatomoVisitSender and the
// subset of matomo/matomo-php-tracker they use (a page view tracked with a POSTed tracking request).
package matomo

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Options ports Matomo\MatomoOptions.
type Options struct {
	Enabled  bool
	BaseURL  *string
	SiteID   *string
	APIToken *string
}

// OptionsFromEnv ports MatomoOptions::fromEnv().
func OptionsFromEnv() Options {
	return Options{
		Enabled:  config.MatomoEnabled.Load().AsBool(),
		BaseURL:  config.MatomoBaseURL.Load().StringOrNil(),
		SiteID:   config.MatomoSiteID.Load().StringOrNil(),
		APIToken: config.MatomoAPIToken.Load().StringOrNil(),
	}
}

// siteID ports MatomoOptions::siteId(): an (int) cast of the configured value.
func (o Options) siteID() *int {
	if o.SiteID == nil {
		return nil
	}
	id := config.PHPIntCast(*o.SiteID)
	return &id
}

// requestTimeout ports MatomoTrackerBuilder::MATOMO_DEFAULT_TIMEOUT.
const requestTimeout = 10 * time.Second

// ErrTrackerNotConfigured ports the RuntimeException MatomoTrackerBuilder throws.
var ErrTrackerNotConfigured = errors.New("Cannot create MatomoTracker. Either site ID, base URL or api token are not defined")

// tracker holds the values matomo-php-tracker sends for one page view, built like MatomoTrackerBuilder configures it.
type tracker struct {
	siteID         int
	apiURL         string
	tokenAuth      string
	createTs       int64
	visitorID      string
	pageURL        string
	urlReferrer    string
	userAgent      string
	ip             string
	forcedDatetime string
	country        string
	city           string
	lat            float64
	long           float64
	customParams   [][2]string
}

// randomHex ports substr(md5(uniqid(rand(), true)), 0, n).
func randomHex(n int) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	sum := md5.Sum(b[:])
	return hex.EncodeToString(sum[:])[:n]
}

// mtRand ports substr(strval(mt_rand()), 2, 6): mt_rand() returns 0..2147483647.
func mtRand() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(2147483648))
	s := n.String()
	if len(s) <= 2 {
		return ""
	}
	s = s[2:]
	if len(s) > 6 {
		s = s[:6]
	}
	return s
}

// baseURL ports MatomoTracker::getBaseUrl().
func (t *tracker) baseURL() string {
	u := t.apiURL
	if !strings.Contains(u, "/matomo.php") && !strings.Contains(u, "/proxy-matomo.php") {
		u = strings.TrimRight(u, "/") + "/matomo.php"
	}
	return u
}

// phpFloatString renders a float like PHP's string conversion (precision=14).
func phpFloatString(f float64) string {
	return strconv.FormatFloat(f, 'G', 14, 64)
}

// pageViewRequest ports getUrlTrackPageView() and the POST handling of sendRequest(): the endpoint and the
// url-encoded body.
func (t *tracker) pageViewRequest(documentTitle, pageviewID, r string) (string, string) {
	var b strings.Builder
	b.WriteString("idsite=" + strconv.Itoa(t.siteID))
	b.WriteString("&rec=1&apiv=1&r=" + r)
	if t.ip != "" && t.ip != "0" && t.tokenAuth != "" {
		b.WriteString("&cip=" + t.ip)
	}
	if t.forcedDatetime != "" && t.forcedDatetime != "0" {
		b.WriteString("&cdt=" + phpx.URLEncode(t.forcedDatetime))
	}
	b.WriteString("&_idts=" + strconv.FormatInt(t.createTs, 10))
	b.WriteString("&_id=" + t.visitorID)
	b.WriteString("&url=" + phpx.URLEncode(t.pageURL))
	b.WriteString("&urlref=" + phpx.URLEncode(t.urlReferrer))
	if pageviewID != "" {
		b.WriteString("&pv_id=" + phpx.URLEncode(pageviewID))
	}
	if t.country != "" && t.country != "0" {
		b.WriteString("&country=" + phpx.URLEncode(t.country))
	}
	if t.city != "" && t.city != "0" {
		b.WriteString("&city=" + phpx.URLEncode(t.city))
	}
	if t.lat != 0 {
		b.WriteString("&lat=" + phpx.URLEncode(phpFloatString(t.lat)))
	}
	if t.long != 0 {
		b.WriteString("&long=" + phpx.URLEncode(phpFloatString(t.long)))
	}
	if len(t.customParams) > 0 {
		parts := make([]string, len(t.customParams))
		for i, p := range t.customParams {
			parts[i] = phpx.URLEncode(p[0]) + "=" + phpx.URLEncode(p[1])
		}
		b.WriteString("&" + strings.Join(parts, "&"))
	}
	if documentTitle != "" {
		b.WriteString("&action_name=" + phpx.URLEncode(documentTitle))
	}
	if t.tokenAuth != "" {
		b.WriteString("&token_auth=" + phpx.URLEncode(t.tokenAuth))
	}
	return t.baseURL(), b.String()
}

// VisitSender ports MatomoVisitSender.
type VisitSender struct {
	Options     Options
	Store       *core.Store
	Stringifier core.ShortURLStringifier
	Client      *http.Client
	Now         func() time.Time
}

// NewVisitSender creates a sender with an HTTP client using the tracker's timeouts.
func NewVisitSender(o Options, store *core.Store, s core.ShortURLStringifier) *VisitSender {
	return &VisitSender{Options: o, Store: store, Stringifier: s, Client: &http.Client{Timeout: requestTimeout}, Now: time.Now}
}

// buildTracker ports MatomoTrackerBuilder::buildMatomoTracker().
func (s *VisitSender) buildTracker() (*tracker, error) {
	siteID := s.Options.siteID()
	if siteID == nil || s.Options.BaseURL == nil || s.Options.APIToken == nil {
		return nil, ErrTrackerNotConfigured
	}
	return &tracker{
		siteID: *siteID, apiURL: *s.Options.BaseURL, tokenAuth: *s.Options.APIToken, createTs: s.Now().Unix(),
		visitorID: randomHex(16),
	}, nil
}

// SendVisit ports MatomoVisitSender::sendVisit(). The visit's short URL must be loaded.
func (s *VisitSender) SendVisit(ctx context.Context, v *core.Visit, originalIPAddress *string) error {
	t, err := s.buildTracker()
	if err != nil {
		return err
	}
	t.pageURL = s.urlToTrack(v)
	t.customParams = append(t.customParams, [2]string{"type", string(v.Type)})
	t.userAgent = v.UserAgent
	t.urlReferrer = v.Referer
	t.forcedDatetime = v.Date.UTC().Format("2006-01-02 15:04:05")
	if l := v.Location; l != nil {
		t.city = l.CityName
		t.country = strings.ToLower(l.CountryCode)
		t.lat = l.Latitude
		t.long = l.Longitude
	}
	ip := originalIPAddress
	if ip == nil {
		ip = v.RemoteAddr
	}
	if ip != nil {
		t.ip = *ip
	}
	if v.IsOrphan() {
		t.customParams = append(t.customParams, [2]string{"orphan", "true"})
	}
	title := ""
	if v.ShortURL != nil && v.ShortURL.Title != nil {
		title = *v.ShortURL.Title
	}
	endpoint, body := t.pageViewRequest(title, randomHex(6), mtRand())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	} else {
		req.Header["User-Agent"] = nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// urlToTrack ports resolveUrlToTrack().
func (s *VisitSender) urlToTrack(v *core.Visit) string {
	if v.ShortURL == nil {
		if v.VisitedURL == nil {
			return ""
		}
		return *v.VisitedURL
	}
	return s.Stringifier.Stringify(v.ShortURL.Identifier())
}

// ProgressTracker ports VisitSendingProgressTrackerInterface.
type ProgressTracker interface {
	Success(index int)
	Error(index int, err error)
}

// SendVisitsResult ports Matomo\Model\SendVisitsResult.
type SendVisitsResult struct {
	SuccessfulVisits int
	FailedVisits     int
}

// HasSuccesses ports SendVisitsResult::hasSuccesses().
func (r SendVisitsResult) HasSuccesses() bool { return r.SuccessfulVisits > 0 }

// HasFailures ports SendVisitsResult::hasFailures().
func (r SendVisitsResult) HasFailures() bool { return r.FailedVisits > 0 }

// Count ports SendVisitsResult::count().
func (r SendVisitsResult) Count() int { return r.SuccessfulVisits + r.FailedVisits }

// SendVisitsInDateRange ports MatomoVisitSender::sendVisitsInDateRange().
func (s *VisitSender) SendVisitsInDateRange(ctx context.Context, dateRange core.DateRange, progress ProgressTracker) (SendVisitsResult, error) {
	var result SendVisitsResult
	err := s.Store.IterateVisits(ctx, core.AllVisits, dateRange, true, func(index int, v *core.Visit) error {
		if err := s.SendVisit(ctx, v, nil); err != nil {
			if progress != nil {
				progress.Error(index, err)
			}
			result.FailedVisits++
			return nil
		}
		if progress != nil {
			progress.Success(index)
		}
		result.SuccessfulVisits++
		return nil
	})
	return result, err
}

// Listener ports EventDispatcher\Matomo\SendVisitToMatomo.
type Listener struct {
	Sender *VisitSender
	Logger *slog.Logger
}

// Handle sends a tracked visit to Matomo, logging instead of failing.
func (l *Listener) Handle(ctx context.Context, visitID int64, originalIPAddress *string) {
	if !l.Sender.Options.Enabled {
		return
	}
	v, err := l.Sender.Store.FindVisitByID(ctx, visitID)
	if err == nil && v == nil {
		l.Logger.Warn(fmt.Sprintf(`Tried to send visit with id "%d" to matomo, but it does not exist.`, visitID), "visitId", visitID)
		return
	}
	if err == nil {
		err = l.Sender.SendVisit(ctx, v, originalIPAddress)
	}
	if err != nil {
		l.Logger.Error(fmt.Sprintf("An error occurred while trying to send visit to Matomo. %v", err), "e", err)
	}
}
