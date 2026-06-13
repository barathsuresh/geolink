// internal/kafka/producer.go
// Kafka producer wrapper using segmentio/kafka-go.
package kafka

import (
	"context"
	"log/slog"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// Producer wraps a kafka-go Writer and publishes messages to a single topic.
type Producer struct {
	writer *kafka.Writer
}

// NewProducer creates a Kafka producer writing to the given topic.
// Async=true: WriteMessages returns immediately; broker delivery happens in the
// background. Search events are analytics telemetry — delivery latency must not
// block the HTTP response on the hot path.
func NewProducer(brokers []string, topic string) *Producer {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
		RequiredAcks: kafka.RequireOne,
		MaxAttempts:  3,
		Async:        true,
		BatchBytes:   10 * 1024 * 1024,
		Completion: func(msgs []kafka.Message, err error) {
			if err != nil {
				slog.Warn("kafka produce failed", "err", err, "dropped", len(msgs))
			}
		},
	}
	return &Producer{writer: w}
}

// Produce enqueues a message for async delivery. Always returns nil — errors
// are reported via the Completion callback above.
func (p *Producer) Produce(ctx context.Context, key string, value []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: value,
	})
}

// Close flushes pending messages and closes the underlying writer.
func (p *Producer) Close() error {
	return p.writer.Close()
}
