// internal/kafka/topics.go
// Kafka topic name constants and topic creation helpers.
package kafka

import (
	"context"
	"fmt"

	kafka "github.com/segmentio/kafka-go"
)

const (
	// TopicGeoRawEntries receives raw GeoName batches from the importer.
	// Consumed by the indexer worker.
	TopicGeoRawEntries = "geo.raw.entries"

	// TopicSearchQueries receives search events from the API.
	// Consumed by the personalizer worker.
	TopicSearchQueries = "search.queries"
)

// EnsureTopics creates the given topic names if they do not already exist.
// In KRaft single-node mode the broker is also the controller, so we dial
// it directly and issue CreateTopics without a controller-redirect step
// (which would return the internal kafka:29093 address unreachable from the host).
// Already-existing topics are silently ignored.
func EnsureTopics(ctx context.Context, brokers []string, topicNames ...string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("no brokers provided")
	}

	conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("kafka dial %s: %w", brokers[0], err)
	}
	defer conn.Close()

	var topicConfigs []kafka.TopicConfig
	for _, name := range topicNames {
		topicConfigs = append(topicConfigs, kafka.TopicConfig{
			Topic:             name,
			NumPartitions:     4,
			ReplicationFactor: 1,
		})
	}

	err = conn.CreateTopics(topicConfigs...)
	if err != nil && !isTopicExistsErr(err) {
		return fmt.Errorf("create topics: %w", err)
	}
	return nil
}

// isTopicExistsErr returns true when the error indicates the topic already exists.
func isTopicExistsErr(err error) bool {
	if err == nil {
		return false
	}
	if ke, ok := err.(kafka.Error); ok {
		return ke == kafka.TopicAlreadyExists
	}
	return false
}

