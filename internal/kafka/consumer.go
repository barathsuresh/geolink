// internal/kafka/consumer.go
// Kafka consumer wrapper using segmentio/kafka-go.
package kafka

import (
	"context"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// Consumer wraps a kafka-go Reader for a single topic + consumer group.
type Consumer struct {
	reader *kafka.Reader
}

// NewConsumer creates a Kafka consumer for the given topic and consumer group.
// CommitInterval=0 enables manual offset commits (call CommitMessage explicitly).
func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10 << 20, // 10 MB
		CommitInterval: 0,        // manual commits only
		StartOffset:    kafka.FirstOffset,
		MaxWait:        5 * time.Second,
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: 1 * time.Second,
	})
	return &Consumer{reader: r}
}

// ReadMessage blocks until the next message is available or ctx is cancelled.
func (c *Consumer) ReadMessage(ctx context.Context) (kafka.Message, error) {
	return c.reader.FetchMessage(ctx)
}

// CommitMessage commits the offset for the given message.
// Call this only after the message has been successfully processed.
func (c *Consumer) CommitMessage(ctx context.Context, msg kafka.Message) error {
	return c.reader.CommitMessages(ctx, msg)
}

// Close stops the reader and releases resources.
func (c *Consumer) Close() error {
	return c.reader.Close()
}
