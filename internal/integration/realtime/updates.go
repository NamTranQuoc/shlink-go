// Package realtime ports Shlink's real-time updates: the payloads PublishingUpdatesGenerator builds and the Mercure,
// RabbitMQ and Redis pub/sub publishing helpers from shlink-common.
package realtime

import (
	"context"
	"fmt"

	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Topics, ported from EventDispatcher\Topic.
const (
	TopicNewVisit         = "https://shlink.io/new-visit"
	TopicNewOrphanVisit   = "https://shlink.io/new-orphan-visit"
	TopicNewShortURL      = "https://shlink.io/new-short-url"
	topicNewShortURLVisit = "https://shlink.io/new-visit/%s"
)

// NewShortURLVisitTopic ports Topic::newShortUrlVisit().
func NewShortURLVisitTopic(shortCode *string) string {
	code := ""
	if shortCode != nil {
		code = *shortCode
	}
	return fmt.Sprintf(topicNewShortURLVisit, code)
}

// Update ports shlink-common's UpdatePublishing\Update.
type Update struct {
	Topic   string
	Payload phpx.Map
}

// EncodedPayload ports Shlinkio\Shlink\Json\json_encode(): plain json_encode() flags (escaped slashes and unicode).
func (u Update) EncodedPayload() string {
	return string(phpx.Encode(u.Payload, 0))
}

// Publisher ports PublishingHelperInterface.
type Publisher interface {
	PublishUpdate(ctx context.Context, u Update) error
}

// Generator ports PublishingUpdatesGenerator.
type Generator struct {
	Store       *core.Store
	Transformer core.ShortURLDataTransformer
}

func (g *Generator) transformShortURL(ctx context.Context, su *core.ShortURL) (any, error) {
	if su == nil {
		return []any{}, nil
	}
	deps, err := g.Store.WithDeps(ctx, su)
	if err != nil {
		return nil, err
	}
	return g.Transformer.Transform(deps), nil
}

// NewVisitUpdate ports newVisitUpdate().
func (g *Generator) NewVisitUpdate(ctx context.Context, v *core.Visit) (Update, error) {
	su, err := g.transformShortURL(ctx, v.ShortURL)
	if err != nil {
		return Update{}, err
	}
	return Update{Topic: TopicNewVisit, Payload: phpx.Map{{Key: "shortUrl", Value: su}, {Key: "visit", Value: v}}}, nil
}

// NewOrphanVisitUpdate ports newOrphanVisitUpdate().
func (g *Generator) NewOrphanVisitUpdate(_ context.Context, v *core.Visit) (Update, error) {
	return Update{Topic: TopicNewOrphanVisit, Payload: phpx.Map{{Key: "visit", Value: v}}}, nil
}

// NewShortURLVisitUpdate ports newShortUrlVisitUpdate().
func (g *Generator) NewShortURLVisitUpdate(ctx context.Context, v *core.Visit) (Update, error) {
	var code *string
	if v.ShortURL != nil {
		code = &v.ShortURL.ShortCode
	}
	su, err := g.transformShortURL(ctx, v.ShortURL)
	if err != nil {
		return Update{}, err
	}
	return Update{Topic: NewShortURLVisitTopic(code), Payload: phpx.Map{{Key: "shortUrl", Value: su}, {Key: "visit", Value: v}}}, nil
}

// NewShortURLUpdate ports newShortUrlUpdate().
func (g *Generator) NewShortURLUpdate(ctx context.Context, su *core.ShortURL) (Update, error) {
	deps, err := g.Store.WithDeps(ctx, su)
	if err != nil {
		return Update{}, err
	}
	return Update{Topic: TopicNewShortURL, Payload: phpx.Map{{Key: "shortUrl", Value: g.Transformer.Transform(deps)}}}, nil
}
