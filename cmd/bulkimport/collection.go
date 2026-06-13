package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/typesense/typesense-go/typesense"
	"github.com/typesense/typesense-go/typesense/api"
)

func initCollection(client *typesense.Client, collectionName string) error {
	ctx := context.Background()
	if _, err := client.Collection(collectionName).Retrieve(ctx); err == nil {
		slog.Info("typesense collection already exists", "collection", collectionName)
		return nil
	}
	t, f := true, false
	schema := &api.CollectionSchema{
		Name: collectionName,
		Fields: []api.Field{
			{Name: "geoname_id",      Type: "int64"},
			{Name: "name",            Type: "string"},
			{Name: "ascii_name",      Type: "string"},
			{Name: "alternate_names", Type: "string",  Optional: &t},
			{Name: "country_code",    Type: "string",  Facet: &t},
			{Name: "feature_code",    Type: "string",  Facet: &t},
			{Name: "feature_class",   Type: "string",  Facet: &t},
			{Name: "population",      Type: "int64",   Facet: &f},
			{Name: "latitude",        Type: "float"},
			{Name: "longitude",       Type: "float"},
			{Name: "timezone",        Type: "string",  Optional: &t},
		},
		DefaultSortingField: strPtr("population"),
	}
	if _, err := client.Collections().Create(ctx, schema); err != nil {
		return fmt.Errorf("create collection %q: %w", collectionName, err)
	}
	slog.Info("typesense collection created", "collection", collectionName)
	return nil
}

func strPtr(s string) *string { return &s }
