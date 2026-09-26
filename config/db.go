package config

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var MongoClient *mongo.Client
var MongoDB *mongo.Database

func InitDB(mongoURI string) (*mongo.Database, error) {
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI is not set in environment variables")
	}

	dbName := extractDatabaseName(mongoURI, "db_two")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientOpts := options.Client().
		ApplyURI(mongoURI).
		SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mongo client: %w", err)
	}

	if err := client.Ping(ctx, readpref.PrimaryPreferred()); err != nil {
		return nil, fmt.Errorf("failed to ping mongo cluster: %w", err)
	}

	MongoClient = client
	MongoDB = client.Database(dbName)
	slog.Info("Connected to MongoDB successfully", "database", dbName)

	return MongoDB, nil
}

func extractDatabaseName(uri, fallback string) string {
	parts := strings.Split(uri, "?")
	base := parts[0]
	lastSlash := strings.LastIndex(base, "/")
	if lastSlash != -1 && lastSlash < len(base)-1 {
		name := base[lastSlash+1:]
		if name != "" && !strings.Contains(name, "@") {
			return name
		}
	}
	return fallback
}
