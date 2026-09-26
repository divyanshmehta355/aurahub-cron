package config

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                  string
	MongoURI              string
	RedisURL              string
	AuraAPIBaseURL        string
	UploadFolderID        string
	CronSecret            string
	AgingMinutesThreshold float64
	MaxClonesPerRun       int64
	AutoCloneIntervalHours int
}

func LoadConfig() *Config {
	// Load .env if present (ignored in production environments like Render)
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, using system environment variables")
	}

	port := getEnv("PORT", "4000")
	mongoURI := os.Getenv("MONGO_URI")
	redisURL := os.Getenv("REDIS_URL")
	auraBase := getEnv("AURA_API_BASE_URL", "https://aurahub-api-hono.ashwathama249.workers.dev")
	folderID := getEnv("UPLOAD_FOLDER_ID", "QU3yuiRZZFw")

	cronSecret := os.Getenv("CRON_SECRET")
	if cronSecret == "" {
		cronSecret = getEnv("CRON_SECRET_KEY", "aurahub_cron_secret_key_2026")
	}

	thresholdMinutes, err := strconv.ParseFloat(getEnv("AGING_MINUTES_THRESHOLD", "60"), 64)
	if err != nil {
		thresholdMinutes = 60
	}

	maxClones, err := strconv.ParseInt(getEnv("MAX_CLONES_PER_RUN", "5"), 10, 64)
	if err != nil {
		maxClones = 5
	}

	intervalHours, _ := strconv.Atoi(getEnv("AUTO_CLONE_INTERVAL_HOURS", "0"))

	return &Config{
		Port:                  port,
		MongoURI:              mongoURI,
		RedisURL:              redisURL,
		AuraAPIBaseURL:        auraBase,
		UploadFolderID:        folderID,
		CronSecret:            cronSecret,
		AgingMinutesThreshold: thresholdMinutes,
		MaxClonesPerRun:       maxClones,
		AutoCloneIntervalHours: intervalHours,
	}
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
