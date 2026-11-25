// Package rabbitmq is the RabbitMQ implementation of queue.Publisher and queue.Consumer.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/queue"
)

// Client holds one connection and channel.
type Client struct {
	conn      *amqp.Connection
	ch        *amqp.Channel
	queueName string
	dlqName   string
}

// Dial connects and declares the main queue and the dead-letter queue.
// Both queues are durable so messages survive a RabbitMQ restart.
func Dial(url, queueName, dlqName string) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	for _, name := range []string{queueName, dlqName} {
		if _, err := ch.QueueDeclare(name, true, false, false, false, nil); err != nil {
			conn.Close()
			return nil, fmt.Errorf("declare queue %s: %w", name, err)
		}
	}
	// Process one message at a time per worker; scale by running more workers.
	if err := ch.Qos(1, 0, false); err != nil {
		conn.Close()
		return nil, err
	}
	return &Client{conn: conn, ch: ch, queueName: queueName, dlqName: dlqName}, nil
}

func (c *Client) Close() error {
	_ = c.ch.Close()
	return c.conn.Close()
}

func (c *Client) publish(ctx context.Context, q string, body any, headers amqp.Table) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.ch.PublishWithContext(ctx, "", q, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Headers:      headers,
		Body:         b,
	})
}

func (c *Client) Publish(ctx context.Context, job domain.EvaluationJob) error {
	return c.publish(ctx, c.queueName, job, nil)
}

func (c *Client) PublishDeadLetter(ctx context.Context, job domain.EvaluationJob, reason string) error {
	return c.publish(ctx, c.dlqName, job, amqp.Table{"x-failure-reason": reason})
}

// Consume blocks until ctx is cancelled. Messages are acked after the
// handler returns, so a crash mid-processing re-delivers the message.
// Retries are handled by the worker (it re-publishes with Attempt+1),
// so the message is always acked here.
func (c *Client) Consume(ctx context.Context, h queue.Handler) error {
	msgs, err := c.ch.Consume(c.queueName, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-msgs:
			if !ok {
				return fmt.Errorf("rabbitmq channel closed")
			}
			var job domain.EvaluationJob
			if err := json.Unmarshal(m.Body, &job); err != nil {
				slog.Error("invalid message, sending to dead-letter queue", "error", err)
				_ = c.ch.PublishWithContext(ctx, "", c.dlqName, false, false, amqp.Publishing{
					ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: m.Body,
					Headers: amqp.Table{"x-failure-reason": "invalid json: " + err.Error()},
				})
				_ = m.Ack(false)
				continue
			}
			_ = h(ctx, job)
			_ = m.Ack(false)
		}
	}
}
