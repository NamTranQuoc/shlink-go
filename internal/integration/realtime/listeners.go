package realtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
)

// Remote system names, ported from EventDispatcher\Async\RemoteSystem.
const (
	SystemMercure      = "Mercure"
	SystemRabbitMQ     = "RabbitMQ"
	SystemRedisPubSub  = "Redis pub/sub"
	logContextError    = "e"
	logContextSystem   = "name"
	logContextVisitID  = "visitId"
	logContextShortURL = "shortUrlId"
)

// Notifier ports AbstractNotifyVisitListener and AbstractNotifyNewShortUrlListener for one remote system.
type Notifier struct {
	System    string
	Publisher Publisher
	Generator *Generator
	Store     *core.Store
	Topics    config.RealTimeUpdatesOptions
	Logger    *slog.Logger
}

// NotifyVisit ports AbstractNotifyVisitListener::__invoke().
func (n *Notifier) NotifyVisit(ctx context.Context, visitID int64) {
	v, err := n.Store.FindVisitByID(ctx, visitID)
	if err != nil {
		n.Logger.Debug(fmt.Sprintf("Error while trying to notify %s with new visit. %v", n.System, err),
			logContextError, err, logContextSystem, n.System)
		return
	}
	if v == nil {
		n.Logger.Warn(fmt.Sprintf(`Tried to notify %s for visit with id "%d", but it does not exist.`, n.System, visitID),
			logContextVisitID, visitID, logContextSystem, n.System)
		return
	}
	updates, err := n.updatesForVisit(ctx, v)
	if err == nil {
		for _, u := range updates {
			if err = n.Publisher.PublishUpdate(ctx, u); err != nil {
				break
			}
		}
	}
	if err != nil {
		n.Logger.Debug(fmt.Sprintf("Error while trying to notify %s with new visit. %v", n.System, err),
			logContextError, err, logContextSystem, n.System)
	}
}

// updatesForVisit ports determineUpdatesForVisit().
func (n *Notifier) updatesForVisit(ctx context.Context, v *core.Visit) ([]Update, error) {
	if v.IsOrphan() {
		if !n.Topics.IsTopicEnabled(config.TopicNewOrphanVisit) {
			return nil, nil
		}
		u, err := n.Generator.NewOrphanVisitUpdate(ctx, v)
		return []Update{u}, err
	}
	var updates []Update
	if n.Topics.IsTopicEnabled(config.TopicNewShortURLVisit) {
		u, err := n.Generator.NewShortURLVisitUpdate(ctx, v)
		if err != nil {
			return nil, err
		}
		updates = append(updates, u)
	}
	if n.Topics.IsTopicEnabled(config.TopicNewVisit) {
		u, err := n.Generator.NewVisitUpdate(ctx, v)
		if err != nil {
			return nil, err
		}
		updates = append(updates, u)
	}
	return updates, nil
}

// NotifyNewShortURL ports AbstractNotifyNewShortUrlListener::__invoke().
func (n *Notifier) NotifyNewShortURL(ctx context.Context, shortURLID int64) {
	su, err := n.Store.FindShortURLByID(ctx, shortURLID)
	if err == nil && su == nil {
		n.Logger.Warn(fmt.Sprintf(`Tried to notify %s for new short URL with id "%d", but it does not exist.`, n.System,
			shortURLID), logContextShortURL, shortURLID, logContextSystem, n.System)
		return
	}
	if err == nil && !n.Topics.IsTopicEnabled(config.TopicNewShortURL) {
		return
	}
	if err == nil {
		var u Update
		if u, err = n.Generator.NewShortURLUpdate(ctx, su); err == nil {
			err = n.Publisher.PublishUpdate(ctx, u)
		}
	}
	if err != nil {
		n.Logger.Debug(fmt.Sprintf("Error while trying to notify %s with new short URL. %v", n.System, err),
			logContextError, err, logContextSystem, n.System)
	}
}
