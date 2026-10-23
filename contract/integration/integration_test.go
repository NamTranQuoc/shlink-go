// Package integration checks the real-time updates (Redis pub/sub, RabbitMQ, Mercure) and the Matomo tracking requests
// the target emits, by listening on the brokers of the compose `integrations` profile. Messages are normalized and
// compared with goldens captured from upstream (CONTRACT_UPDATE_GOLDEN=1 with the php target). Upstream has no API
// tests for these integrations, so the scenarios are this suite's own.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// Broker endpoints published on the host by docker/compose.yml.
const (
	redisAddr  = "127.0.0.1:16379"
	rabbitURL  = "amqp://rabbit:rabbit@127.0.0.1:15672/"
	mercureURL = "http://127.0.0.1:13080/.well-known/mercure"
	// The hub URL mercure-info returns: MERCURE_PUBLIC_HUB_URL of docker/env/profiles/integrations.env.
	publicHubURL = "http://localhost:13080/.well-known/mercure"
)

const (
	topicNewShortURL    = "https://shlink.io/new-short-url"
	topicNewVisit       = "https://shlink.io/new-visit"
	topicNewOrphanVisit = "https://shlink.io/new-orphan-visit"
)

func topicNewShortURLVisit(shortCode string) string {
	return "https://shlink.io/new-visit/" + shortCode
}

// defaultDomain is DEFAULT_DOMAIN of docker/compose.yml. Visits send it as Host, so visited URLs do not depend on the
// target's published port.
const defaultDomain = "s.test"

const goldenPath = "testdata/integrations.golden.json"

// Message is one normalized update or tracking request.
type Message struct {
	System string `json:"system"`
	Topic  string `json:"topic"`
	// Meta holds transport details compared too (RabbitMQ properties, Matomo headers).
	Meta    map[string]string `json:"meta,omitempty"`
	Payload string            `json:"payload"`
}

func requireProfile(t *testing.T) {
	t.Helper()
	if os.Getenv("CONTRACT_PROFILE") != "integrations" {
		t.Skip("runs only with PROFILE=integrations")
	}
}

var (
	isoNow     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}`)
	matomoNorm = map[string]*regexp.Regexp{
		// substr(strval(mt_rand()), 2, 6): shorter when mt_rand() has fewer than 8 digits
		"r":     regexp.MustCompile(`^\d{0,6}$`),
		"_idts": regexp.MustCompile(`^\d{10}$`),
		"_id":   regexp.MustCompile(`^[0-9a-f]{16}$`),
		"pv_id": regexp.MustCompile(`^[0-9a-f]{6}$`),
		"cdt":   regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`),
	}
)

// normalizePayload replaces timestamps of the current run.
func normalizePayload(s string) string {
	now := time.Now().UTC()
	return isoNow.ReplaceAllStringFunc(s, func(m string) string {
		t, err := time.Parse(time.RFC3339, m)
		if err == nil && t.Sub(now) < time.Hour && now.Sub(t) < time.Hour {
			return "<now>"
		}
		return m
	})
}

// normalizeMatomoParams replaces the random and time-based tracker params, keeping their order. A value that does not
// have the expected shape is left as is, so the comparison fails.
func normalizeMatomoParams(raw string) string {
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		k, v, _ := strings.Cut(p, "=")
		re, ok := matomoNorm[k]
		if !ok {
			continue
		}
		dec, err := url.QueryUnescape(v)
		if err == nil && re.MatchString(dec) {
			parts[i] = k + "=<" + k + ">"
		}
	}
	return strings.Join(parts, "&")
}

// collector listens on every system before a scenario runs.
type collector struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	msgs   []Message
}

func newCollector(t *testing.T, mercureToken string, rabbitTopics ...string) *collector {
	t.Helper()
	h.HelperURL(t)
	h.TakeMatomoRequests()
	ctx, cancel := context.WithCancel(context.Background())
	c := &collector{t: t, ctx: ctx, cancel: cancel}
	t.Cleanup(c.stop)
	c.listenRedis()
	c.listenRabbit(rabbitTopics)
	for _, topic := range rabbitTopics {
		c.listenMercure(topic, mercureToken)
	}
	return c
}

func (c *collector) add(m Message) {
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
}

func (c *collector) stop() {
	c.cancel()
	c.wg.Wait()
}

func (c *collector) listenRedis() {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	sub := client.PSubscribe(c.ctx, "*")
	if _, err := sub.Receive(c.ctx); err != nil {
		c.t.Fatalf("redis subscribe: %v", err)
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer client.Close()
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-c.ctx.Done():
				return
			case m := <-ch:
				c.add(Message{System: "redis", Topic: m.Channel, Payload: normalizePayload(m.Payload)})
			}
		}
	}()
}

// listenRabbit consumes the queues named after the topics. Upstream declares the same durable queue and binds it to
// the topic's exchange on every publish, so declaring it here first is idempotent; it is purged so leftovers of
// earlier runs do not leak in.
func (c *collector) listenRabbit(topics []string) {
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		c.t.Fatalf("rabbitmq dial: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		c.t.Fatalf("rabbitmq channel: %v", err)
	}
	for _, topic := range topics {
		if _, err := ch.QueueDeclare(topic, true, false, false, false, nil); err != nil {
			c.t.Fatalf("rabbitmq declare %s: %v", topic, err)
		}
		if _, err := ch.QueuePurge(topic, false); err != nil {
			c.t.Fatalf("rabbitmq purge %s: %v", topic, err)
		}
		deliveries, err := ch.Consume(topic, "", true, false, false, false, nil)
		if err != nil {
			c.t.Fatalf("rabbitmq consume %s: %v", topic, err)
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.ctx.Done():
					return
				case d, ok := <-deliveries:
					if !ok {
						return
					}
					c.add(Message{
						System: "rabbitmq",
						Topic:  d.Exchange + " -> " + topic,
						Meta: map[string]string{
							"contentType":  d.ContentType,
							"deliveryMode": fmt.Sprint(d.DeliveryMode),
						},
						Payload: normalizePayload(string(d.Body)),
					})
				}
			}
		}()
	}
	go func() {
		<-c.ctx.Done()
		_ = conn.Close()
	}()
}

// listenMercure subscribes to one topic: SSE events do not carry their topic, so each topic gets its own stream.
func (c *collector) listenMercure(topic, token string) {
	req, _ := http.NewRequestWithContext(c.ctx, http.MethodGet, mercureURL+"?topic="+url.QueryEscape(topic), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("mercure subscribe: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("mercure subscribe %s: status %d", topic, resp.StatusCode)
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var data []string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case line == "" && data != nil:
				c.add(Message{System: "mercure", Topic: topic, Payload: normalizePayload(strings.Join(data, "\n"))})
				data = nil
			}
		}
	}()
}

func (c *collector) takeMatomo() {
	for _, r := range h.TakeMatomoRequests() {
		c.add(Message{
			System:  "matomo",
			Topic:   r.Method + " " + h.MatomoTrackingPath,
			Meta:    map[string]string{"userAgent": r.UserAgent, "contentType": r.ContentType},
			Payload: normalizeMatomoParams(r.Query) + "|" + normalizeMatomoParams(r.Body),
		})
	}
}

// wait collects until want messages arrived (then a short grace period for unexpected extras) or the timeout.
func (c *collector) wait(want int) []Message {
	timeout := 15 * time.Second
	if want == 0 {
		// Capturing goldens: nothing to wait for but the async listeners.
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.takeMatomo()
		c.mu.Lock()
		n := len(c.msgs)
		c.mu.Unlock()
		if want > 0 && n >= want {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(time.Second)
	c.takeMatomo()
	c.stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]Message(nil), c.msgs...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.System != b.System {
			return a.System < b.System
		}
		if a.Topic != b.Topic {
			return a.Topic < b.Topic
		}
		return a.Payload < b.Payload
	})
	return out
}

var (
	goldenMu sync.Mutex
	golden   map[string][]Message
)

func loadGolden(t *testing.T) map[string][]Message {
	t.Helper()
	goldenMu.Lock()
	defer goldenMu.Unlock()
	if golden == nil {
		golden = map[string][]Message{}
		if raw, err := os.ReadFile(goldenPath); err == nil {
			if err := json.Unmarshal(raw, &golden); err != nil {
				t.Fatalf("parse %s: %v", goldenPath, err)
			}
		}
	}
	return golden
}

// assertGolden compares the messages of a scenario with upstream's, or records them when capturing goldens.
func assertGolden(t *testing.T, scenario string, c *collector) {
	t.Helper()
	update := os.Getenv("CONTRACT_UPDATE_GOLDEN") == "1"
	if update && os.Getenv("CONTRACT_TARGET") != "php" {
		t.Fatal("goldens can only be captured from the upstream target")
	}
	g := loadGolden(t)
	want, ok := g[scenario]
	if !ok && !update {
		t.Fatalf("no golden for scenario %q", scenario)
	}
	got := c.wait(len(want))
	if update {
		goldenMu.Lock()
		g[scenario] = got
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err := enc.Encode(g)
		goldenMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	h.AssertEquals(t, toAny(t, want), toAny(t, got), scenario)
}

func toAny(t *testing.T, msgs []Message) any {
	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// subscriptionToken fetches the token mercure-info hands to clients.
func subscriptionToken(t *testing.T) string {
	t.Helper()
	resp := h.CallAPIWithKey(t, http.MethodGet, "/rest/v3/mercure-info")
	h.AssertStatus(t, resp, http.StatusOK)
	token, _ := resp.JSON(t)["token"].(string)
	if token == "" {
		t.Fatalf("mercure-info returned no token: %s", resp.Body)
	}
	return token
}

func TestNewShortURLIsPublished(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	c := newCollector(t, subscriptionToken(t), topicNewShortURL)

	resp := h.CallAPIWithKey(t, http.MethodPost, "/rest/v3/short-urls", h.Options{JSON: map[string]any{
		"longUrl":    "https://shlink.io/documentation/",
		"customSlug": "integration-slug",
		"tags":       []string{"foo", "baz"},
		"title":      "Integration title",
	}})
	h.AssertStatus(t, resp, http.StatusOK)

	assertGolden(t, "newShortUrl", c)
}

func TestShortURLVisitIsPublishedAndTracked(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	c := newCollector(t, subscriptionToken(t), topicNewVisit, topicNewShortURLVisit("def456"))

	resp := h.CallShortURL(t, "def456", h.Options{Headers: http.Header{
		"Host":            {defaultDomain},
		"User-Agent":      {h.AndroidUserAgent},
		"Referer":         {"https://example.com/from"},
		"X-Forwarded-For": {"1.2.3.4"},
	}})
	h.AssertStatus(t, resp, http.StatusFound)

	assertGolden(t, "shortUrlVisit", c)
}

func TestOrphanVisitIsPublishedAndTracked(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	c := newCollector(t, subscriptionToken(t), topicNewOrphanVisit)

	resp := h.Request(t, http.MethodGet, "/integration/not-found?foo=bar", h.Options{Headers: http.Header{
		"Host":            {defaultDomain},
		"User-Agent":      {h.IOSUserAgent},
		"X-Forwarded-For": {"5.6.7.8"},
	}})
	h.AssertStatus(t, resp, http.StatusNotFound)

	assertGolden(t, "orphanVisit", c)
}

func TestMercureInfo(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	before := time.Now()
	resp := h.CallAPIWithKey(t, http.MethodGet, "/rest/v3/mercure-info")
	h.AssertStatus(t, resp, http.StatusOK)
	body := resp.JSON(t)
	h.AssertEquals(t, publicHubURL, body["mercureHubUrl"])

	// Upstream signs the token for jwt_days_duration (1 day by default), rounded to the second.
	exp, err := time.Parse(time.RFC3339, fmt.Sprint(body["jwtExpiration"]))
	if err != nil {
		t.Fatalf("jwtExpiration: %v", err)
	}
	if d := exp.Sub(before.Add(24 * time.Hour)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("jwtExpiration %s is not one day from now", exp)
	}

	token, _ := body["token"].(string)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token is not a JWT: %q", token)
	}
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	h.AssertEquals(t, `{"typ":"JWT","alg":"HS256"}`, string(header))
	var c map[string]any
	if err := json.Unmarshal(claims, &c); err != nil {
		t.Fatalf("token claims: %v", err)
	}
	h.AssertEquals(t, "Shlink", c["iss"])
	h.AssertEquals(t, map[string]any{"subscribe": []any{"*"}}, c["mercure"])
	h.AssertEquals(t, float64(exp.Unix()), c["exp"])

	// The hub accepts the token for a subscription and rejects a tampered one.
	h.AssertEquals(t, http.StatusOK, mercureSubscribeStatus(t, token))
	h.AssertEquals(t, http.StatusUnauthorized, mercureSubscribeStatus(t, parts[0]+"."+parts[1]+".invalid"))
}

func mercureSubscribeStatus(t *testing.T, token string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, mercureURL+"?topic="+url.QueryEscape(topicNewVisit), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mercure subscribe: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
