package realtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// MercureOptions ports shlink-common's MercureOptions, built from the `mercure` config block.
type MercureOptions struct {
	Enabled        bool
	PublicHubURL   *string
	InternalHubURL *string
	JWTSecret      *string
	JWTIssuer      string
	// JWTDaysDuration ports MercureInfoAction's `jwt_days_duration` config value (not set by Shlink, so 1).
	JWTDaysDuration int
}

// MercureOptionsFromEnv ports config/autoload/mercure.global.php.
func MercureOptionsFromEnv() MercureOptions {
	return MercureOptions{
		Enabled:         config.MercureEnabled.Load().AsBool(),
		PublicHubURL:    config.MercurePublicHubURL.Load().StringOrNil(),
		InternalHubURL:  config.MercureInternalHubURL.Load().StringOrNil(),
		JWTSecret:       config.MercureJWTSecret.Load().StringOrNil(),
		JWTIssuer:       "Shlink",
		JWTDaysDuration: 1,
	}
}

// ErrMissingJWTSecret ports MercureException::missingJwtSecret().
var ErrMissingJWTSecret = errors.New("You have to provide a non-empty secret key for the JWT generation, under mercure.jwt_secret")

// ErrMissingHubURL ports MercureException::missingHubUrl().
var ErrMissingHubURL = errors.New("You have to provide mercure hub URL under mercure.internal_hub_url or mercure.public_hub_url")

// Mercure ports LcobucciJwtProvider, the Mercure Hub and MercureHubPublishingHelper. It also provides what the
// mercure-info endpoint needs.
type Mercure struct {
	Options MercureOptions
	Client  *http.Client
	// Now returns the current time; replaceable for tests.
	Now func() time.Time
}

// NewMercure creates the Mercure integration.
func NewMercure(o MercureOptions) *Mercure {
	return &Mercure{Options: o, Client: &http.Client{}, Now: time.Now}
}

// PublicHubURL implements web.MercureTokens.
func (m *Mercure) PublicHubURL() *string { return m.Options.PublicHubURL }

// JWTDays implements web.MercureTokens.
func (m *Mercure) JWTDays() int { return m.Options.JWTDaysDuration }

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// buildToken ports LcobucciJwtProvider::buildToken(): HS256 signed with the configured secret, `typ` and `alg`
// headers, then iss, iat, exp and the mercure claim. Dates are rounded down to the second.
func (m *Mercure) buildToken(mercureClaim phpx.Map, expiresAt time.Time) (string, error) {
	if m.Options.JWTSecret == nil || *m.Options.JWTSecret == "" || *m.Options.JWTSecret == "0" {
		return "", ErrMissingJWTSecret
	}
	if bits := len(*m.Options.JWTSecret) * 8; bits < 256 {
		// Lcobucci's HMAC signer rejects short keys (InvalidKeyProvided::tooShort())
		return "", fmt.Errorf("Key provided is shorter than 256 bits, only %d bits provided", bits)
	}
	// Lcobucci's JoseEncoder uses JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE
	flags := phpx.JSONUnescapedSlashes | phpx.JSONUnescapedUnicode
	header := phpx.Encode(phpx.Map{{Key: "typ", Value: "JWT"}, {Key: "alg", Value: "HS256"}}, flags)
	claims := phpx.Encode(phpx.Map{
		{Key: "iss", Value: m.Options.JWTIssuer},
		{Key: "iat", Value: m.Now().Unix()},
		{Key: "exp", Value: expiresAt.Unix()},
		{Key: "mercure", Value: mercureClaim},
	}, flags)
	payload := base64URL(header) + "." + base64URL(claims)
	mac := hmac.New(sha256.New, []byte(*m.Options.JWTSecret))
	mac.Write([]byte(payload))
	return payload + "." + base64URL(mac.Sum(nil)), nil
}

// BuildPublishToken ports LcobucciJwtProvider::buildPublishToken(): valid for 10 minutes.
func (m *Mercure) BuildPublishToken() (string, error) {
	return m.buildToken(phpx.Map{{Key: "publish", Value: []string{"*"}}}, m.Now().Add(10*time.Minute))
}

// BuildSubscriptionToken implements web.MercureTokens, porting LcobucciJwtProvider::buildSubscriptionToken().
func (m *Mercure) BuildSubscriptionToken(expiresAt time.Time) (string, error) {
	return m.buildToken(phpx.Map{{Key: "subscribe", Value: []string{"*"}}}, expiresAt)
}

// hubURL ports HubFactory: the internal hub URL, falling back to the public one.
func (m *Mercure) hubURL() (string, error) {
	hub := m.Options.InternalHubURL
	if hub == nil {
		hub = m.Options.PublicHubURL
	}
	if hub == nil {
		return "", ErrMissingHubURL
	}
	return *hub + "/.well-known/mercure", nil
}

var jwtPattern = regexp.MustCompile(`^[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]*$`)

// PublishUpdate ports MercureHubPublishingHelper::publishUpdate() and Symfony's Hub::publish().
func (m *Mercure) PublishUpdate(ctx context.Context, u Update) error {
	hub, err := m.hubURL()
	if err != nil {
		return err
	}
	jwt, err := m.BuildPublishToken()
	if err != nil {
		return err
	}
	if !jwtPattern.MatchString(jwt) {
		return errors.New("The provided JWT is not valid.")
	}
	body := "topic=" + phpx.URLEncode(u.Topic) + "&data=" + phpx.URLEncode(u.EncodedPayload())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hub, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.Client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send an update.: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// Symfony's getContent() throws for 3xx/4xx/5xx responses
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Failed to send an update.: HTTP %d returned for %q", resp.StatusCode, hub)
	}
	return nil
}
