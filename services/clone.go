package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"aurahub-cron/config"
	"aurahub-cron/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AutoCloneReport struct {
	Success          bool      `json:"success"`
	ThresholdMinutes float64   `json:"thresholdMinutes"`
	AgingCutoffDate  time.Time `json:"agingCutoffDate"`
	Timestamp        time.Time `json:"timestamp"`
	ClonesFinalized  int       `json:"clonesFinalized"`
	ClonesInitiated  int       `json:"clonesInitiated"`
	DeadVideosFound  int       `json:"deadVideosFound"`
	DurationMs       int64     `json:"durationMs"`
	Logs             []string  `json:"logs"`
	Error            string    `json:"error,omitempty"`
}

var runLock sync.Mutex

// RunAutoCloneCycle executes the complete 2-phase auto-clone cycle
func RunAutoCloneCycle(ctx context.Context, cfg *config.Config) *AutoCloneReport {
	// Prevent overlapping runs
	if !runLock.TryLock() {
		return &AutoCloneReport{
			Success:   false,
			Error:     "A clone cycle is already currently executing",
			Timestamp: time.Now().UTC(),
		}
	}
	defer runLock.Unlock()

	startTime := time.Now()
	cutoff := startTime.Add(-time.Duration(cfg.AgingMinutesThreshold * float64(time.Minute)))

	report := &AutoCloneReport{
		Success:          true,
		ThresholdMinutes: cfg.AgingMinutesThreshold,
		AgingCutoffDate:  cutoff,
		Timestamp:        startTime.UTC(),
		Logs:             make([]string, 0),
	}

	addLog := func(msg string) {
		report.Logs = append(report.Logs, msg)
		slog.Info(msg)
	}

	addLog(fmt.Sprintf("Starting auto-clone cycle (threshold: %.0fm, cutoff: %s)",
		cfg.AgingMinutesThreshold, cutoff.Format(time.RFC3339)))

	if config.MongoDB == nil {
		report.Success = false
		report.Error = "MongoDB connection is not initialized"
		report.DurationMs = time.Since(startTime).Milliseconds()
		return report
	}

	collection := config.MongoDB.Collection("videos")

	// -------------------------------------------------------------
	// PHASE 1: Finalize Any In-Flight Remote Upload Clones
	// -------------------------------------------------------------
	addLog("Phase 1: Checking for in-flight pending remote uploads...")

	phase1Filter := bson.M{
		"pendingRemoteUploadId": bson.M{
			"$exists": true,
			"$nin":    []interface{}{nil, ""},
		},
	}

	findOpts := options.Find().SetLimit(10)
	cursor, err := collection.Find(ctx, phase1Filter, findOpts)
	if err != nil {
		addLog(fmt.Sprintf("Phase 1 query failed: %v", err))
	} else {
		var pendingVideos []models.Video
		if err := cursor.All(ctx, &pendingVideos); err != nil {
			addLog(fmt.Sprintf("Phase 1 cursor decode failed: %v", err))
		} else {
			if len(pendingVideos) == 0 {
				addLog("Phase 1: No pending in-flight uploads found.")
			} else {
				addLog(fmt.Sprintf("Phase 1: Found %d pending uploads to check.", len(pendingVideos)))
			}

			for _, video := range pendingVideos {
				if video.PendingRemoteUploadID == nil || *video.PendingRemoteUploadID == "" {
					continue
				}

				uploadID := *video.PendingRemoteUploadID
				addLog(fmt.Sprintf("Checking upload status for \"%s\" (%s) with uploadId: %s",
					video.Title, video.ID.Hex(), uploadID))

				statusResult, err := CheckRemoteCloneStatus(ctx, cfg.AuraAPIBaseURL, uploadID)
				if err != nil {
					addLog(fmt.Sprintf("Error checking upload %s: %v", uploadID, err))
					continue
				}

				if statusResult.Status == "finished" && statusResult.LinkID != "" {
					oldFileID := video.FileID
					newFileID := statusResult.LinkID
					newURL := fmt.Sprintf("https://streamtape.com/v/%s/", newFileID)
					now := time.Now().UTC()

					update := bson.M{
						"$set": bson.M{
							"fileId":                newFileID,
							"streamtapeUrl":         newURL,
							"lastRefreshedAt":       now,
							"pendingRemoteUploadId": nil,
							"streamtapeStatus":      "active",
							"updatedAt":             now,
						},
					}

					_, err := collection.UpdateOne(ctx, bson.M{"_id": video.ID}, update)
					if err != nil {
						addLog(fmt.Sprintf("Failed to update video %s in DB: %v", video.ID.Hex(), err))
						continue
					}

					report.ClonesFinalized++
					addLog(fmt.Sprintf("Finalized in-flight clone for \"%s\": %s -> %s", video.Title, oldFileID, newFileID))

					// Invalidate Redis caches
					config.InvalidateVideoCaches(ctx, video.ID.Hex())
					addLog(fmt.Sprintf("Invalidated Redis cache for video %s", video.ID.Hex()))

					// Delete old file from Streamtape
					if oldFileID != "" && oldFileID != newFileID {
						if DeleteStreamtapeFile(ctx, cfg.AuraAPIBaseURL, oldFileID) {
							addLog(fmt.Sprintf("Deleted old Streamtape file %s", oldFileID))
						} else {
							addLog(fmt.Sprintf("Warning: old file %s deletion failed", oldFileID))
						}
					}
				} else if statusResult.Status == "error" {
					addLog(fmt.Sprintf("Remote clone failed for \"%s\" (%s): %s. Resetting pending status.",
						video.Title, uploadID, statusResult.Error))

					now := time.Now().UTC()
					_, _ = collection.UpdateOne(ctx, bson.M{"_id": video.ID}, bson.M{
						"$set": bson.M{
							"pendingRemoteUploadId": nil,
							"updatedAt":             now,
						},
					})
				} else {
					addLog(fmt.Sprintf("Remote clone for \"%s\" still processing (%s)...", video.Title, statusResult.Status))
				}
			}
		}
	}

	// -------------------------------------------------------------
	// PHASE 2: Identify and Start Cloning for Aging Videos
	// -------------------------------------------------------------
	addLog(fmt.Sprintf("Phase 2: Scanning for videos older than %.0fm (batch: %d)...",
		cfg.AgingMinutesThreshold, cfg.MaxClonesPerRun))

	phase2Filter := bson.M{
		"streamtapeStatus": bson.M{"$ne": "dead"},
		"$or": []bson.M{
			{"pendingRemoteUploadId": bson.M{"$exists": false}},
			{"pendingRemoteUploadId": nil},
			{"pendingRemoteUploadId": ""},
		},
		"$and": []bson.M{
			{
				"$or": []bson.M{
					{"lastRefreshedAt": bson.M{"$lt": cutoff}},
					{"lastRefreshedAt": bson.M{"$exists": false}},
					{"lastRefreshedAt": nil},
				},
			},
		},
	}

	sortOpts := options.Find().
		SetSort(bson.D{{Key: "lastRefreshedAt", Value: 1}, {Key: "createdAt", Value: 1}}).
		SetLimit(cfg.MaxClonesPerRun)

	cursor2, err := collection.Find(ctx, phase2Filter, sortOpts)
	if err != nil {
		addLog(fmt.Sprintf("Phase 2 candidate query failed: %v", err))
	} else {
		var candidates []models.Video
		if err := cursor2.All(ctx, &candidates); err != nil {
			addLog(fmt.Sprintf("Phase 2 candidate decode failed: %v", err))
		} else {
			if len(candidates) == 0 {
				addLog(fmt.Sprintf("Phase 2: No videos older than %.0f minute(s) require cloning.", cfg.AgingMinutesThreshold))
			} else {
				addLog(fmt.Sprintf("Phase 2: Found %d candidate video(s) for auto-cloning.", len(candidates)))
			}

			for _, video := range candidates {
				if video.FileID == "" {
					addLog(fmt.Sprintf("Skipping video \"%s\" (%s): no fileId", video.Title, video.ID.Hex()))
					continue
				}

				sourceURL := video.StreamtapeURL
				if sourceURL == "" {
					sourceURL = fmt.Sprintf("https://streamtape.com/v/%s", video.FileID)
				}

				addLog(fmt.Sprintf("Initiating remote clone for \"%s\" from: %s", video.Title, sourceURL))

				cloneResult, err := StartRemoteClone(ctx, cfg.AuraAPIBaseURL, cfg.UploadFolderID, sourceURL)
				if err != nil {
					addLog(fmt.Sprintf("Error calling remote clone for \"%s\": %v", video.Title, err))
					continue
				}

				if !cloneResult.OK {
					addLog(fmt.Sprintf("Warning: Remote clone failed for \"%s\": %s", video.Title, cloneResult.Error))

					lowerErr := strings.ToLower(cloneResult.Error)
					if strings.Contains(lowerErr, "not found") || strings.Contains(lowerErr, "deleted") || strings.Contains(lowerErr, "404") {
						now := time.Now().UTC()
						_, _ = collection.UpdateOne(ctx, bson.M{"_id": video.ID}, bson.M{
							"$set": bson.M{
								"streamtapeStatus": "dead",
								"updatedAt":        now,
							},
						})
						report.DeadVideosFound++
						addLog(fmt.Sprintf("Marked video \"%s\" as dead in DB.", video.Title))
					}
					continue
				}

				// If Streamtape completed the clone instantly
				if cloneResult.LinkID != "" {
					oldFileID := video.FileID
					newFileID := cloneResult.LinkID
					newURL := fmt.Sprintf("https://streamtape.com/v/%s/", newFileID)
					now := time.Now().UTC()

					update := bson.M{
						"$set": bson.M{
							"fileId":                newFileID,
							"streamtapeUrl":         newURL,
							"lastRefreshedAt":       now,
							"pendingRemoteUploadId": nil,
							"streamtapeStatus":      "active",
							"updatedAt":             now,
						},
					}

					_, err := collection.UpdateOne(ctx, bson.M{"_id": video.ID}, update)
					if err != nil {
						addLog(fmt.Sprintf("DB update failed for video %s: %v", video.ID.Hex(), err))
						continue
					}

					report.ClonesFinalized++
					addLog(fmt.Sprintf("Instant clone finalized for \"%s\": %s -> %s", video.Title, oldFileID, newFileID))

					config.InvalidateVideoCaches(ctx, video.ID.Hex())
					addLog(fmt.Sprintf("Invalidated Redis cache for video %s", video.ID.Hex()))

					if oldFileID != "" && oldFileID != newFileID {
						DeleteStreamtapeFile(ctx, cfg.AuraAPIBaseURL, oldFileID)
					}
				} else if cloneResult.RemoteID != "" {
					now := time.Now().UTC()
					update := bson.M{
						"$set": bson.M{
							"pendingRemoteUploadId": cloneResult.RemoteID,
							"updatedAt":             now,
						},
					}
					_, _ = collection.UpdateOne(ctx, bson.M{"_id": video.ID}, update)

					report.ClonesInitiated++
					addLog(fmt.Sprintf("Queued remote clone for \"%s\" (uploadId: %s)", video.Title, cloneResult.RemoteID))
				}
			}
		}
	}

	report.DurationMs = time.Since(startTime).Milliseconds()
	addLog(fmt.Sprintf("Cycle completed in %dms: %d finalized, %d initiated, %d dead.",
		report.DurationMs, report.ClonesFinalized, report.ClonesInitiated, report.DeadVideosFound))

	return report
}
