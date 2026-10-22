package realtime

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strconv"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/NamTranQuoc/shlink-go/internal/config"
)

// RabbitMQOptions ports the `rabbitmq` config block (config/autoload/rabbit.global.php) and RabbitMqOptions.
type RabbitMQOptions struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Vhost    string
	UseSSL   bool
}

// RabbitMQOptionsFromEnv ports config/autoload/rabbit.global.php.
func RabbitMQOptionsFromEnv() RabbitMQOptions {
	return RabbitMQOptions{
		Enabled:  config.RabbitMQEnabled.Load().AsBool(),
		Host:     config.RabbitMQHost.Load().AsString(),
		Port:     config.RabbitMQPort.Load().AsInt(),
		User:     config.RabbitMQUser.Load().AsString(),
		Password: config.RabbitMQPassword.Load().AsString(),
		Vhost:    config.RabbitMQVhost.Load().AsString(),
		UseSSL:   config.RabbitMQUseSSL.Load().AsBool(),
	}
}

// RabbitMQ ports RabbitMqPublishingHelper: one connection per published update, closed afterwards.
type RabbitMQ struct {
	Options RabbitMQOptions
}

func (r *RabbitMQ) dial() (*amqp.Connection, error) {
	scheme := "amqp"
	if r.Options.UseSSL {
		scheme = "amqps"
	}
	u := url.URL{
		Scheme: scheme,
		User:   url.UserPassword(r.Options.User, r.Options.Password),
		Host:   net.JoinHostPort(r.Options.Host, strconv.Itoa(r.Options.Port)),
		Path:   "/" + url.PathEscape(r.Options.Vhost),
	}
	cfg := amqp.Config{Vhost: r.Options.Vhost}
	if r.Options.UseSSL {
		cfg.TLSClientConfig = &tls.Config{ServerName: r.Options.Host}
	}
	return amqp.DialConfig(u.String(), cfg)
}

// PublishUpdate declares a durable direct exchange and queue named after the topic, binds them and publishes the
// payload as a persistent JSON message.
func (r *RabbitMQ) PublishUpdate(ctx context.Context, u Update) error {
	conn, err := r.dial()
	if err != nil {
		return fmt.Errorf("connecting to RabbitMQ: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	name := u.Topic
	if err := ch.ExchangeDeclare(name, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(name, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(name, "", name, false, nil); err != nil {
		return err
	}
	return ch.PublishWithContext(ctx, name, "", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         []byte(u.EncodedPayload()),
	})
}
