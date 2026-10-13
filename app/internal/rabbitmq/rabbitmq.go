// Package rabbitmq provides the queue publisher and consumer adapters.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"api-memberships/internal/reservation"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Conn bundles a connection and a channel bound to a durable queue.
type Conn struct {
	conn  *amqp.Connection
	ch    *amqp.Channel
	queue string
}

// Connect dials RabbitMQ, opens a channel and declares the durable queue.
func Connect(url, queue string) (*Conn, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, fmt.Errorf("declare queue: %w", err)
	}
	return &Conn{conn: conn, ch: ch, queue: queue}, nil
}

func (c *Conn) Close() error {
	_ = c.ch.Close()
	return c.conn.Close()
}

// Publisher implements reservation.Publisher.
type Publisher struct {
	c *Conn
}

func NewPublisher(c *Conn) *Publisher {
	return &Publisher{c: c}
}

func (p *Publisher) Publish(ctx context.Context, job reservation.Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return p.c.ch.PublishWithContext(ctx, "", p.c.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    job.RequestID,
		Body:         body,
	})
}

// Handler processes one decoded job.
type Handler func(ctx context.Context, job reservation.Job) error

// Consume reads deliveries with the given prefetch using concurrency goroutines
// until ctx is cancelled, then waits for in-flight messages to finish.
func (c *Conn) Consume(ctx context.Context, concurrency, prefetch int, handle Handler) error {
	if err := c.ch.Qos(prefetch, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	const tag = "reservation-worker"
	deliveries, err := c.ch.Consume(c.queue, tag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for d := range deliveries {
				process(ctx, d, handle)
			}
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return errors.New("delivery channel closed by broker")
	case <-ctx.Done():
	}
	// Stop new deliveries; the channel closes once the broker confirms,
	// which ends each goroutine after its current message.
	if err := c.ch.Cancel(tag, false); err != nil {
		log.Printf("cancel consumer: %v", err)
	}
	<-done
	return nil
}

// process acks on success, drops malformed or invalid messages (requeue would
// loop forever) and requeues transient failures such as database errors.
func process(ctx context.Context, d amqp.Delivery, handle Handler) {
	var job reservation.Job
	if err := json.Unmarshal(d.Body, &job); err != nil {
		log.Printf("drop malformed message: %v", err)
		_ = d.Nack(false, false)
		return
	}
	// Use a context detached from shutdown so in-flight work can finish.
	err := handle(context.WithoutCancel(ctx), job)
	switch {
	case err == nil:
		_ = d.Ack(false)
	case errors.Is(err, reservation.ErrValidation):
		log.Printf("drop invalid job %s: %v", job.RequestID, err)
		_ = d.Nack(false, false)
	default:
		log.Printf("requeue job %s: %v", job.RequestID, err)
		_ = d.Nack(false, true)
	}
}
