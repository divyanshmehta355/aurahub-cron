import Video from '../models/Video.js';
import {
  startRemoteClone,
  checkRemoteCloneStatus,
  deleteStreamtapeFile,
} from '../lib/streamtape.js';
import { invalidateVideoCaches } from '../config/redis.js';
import { logger } from '../lib/logger.js';

/**
 * Executes a full Auto-Clone cycle with standard console logging.
 * Uses AGING_MINUTES_THRESHOLD (in minutes) to determine candidate videos.
 */
export async function runAutoCloneCycle() {
  const startTime = Date.now();

  const thresholdMinutes = parseFloat(process.env.AGING_MINUTES_THRESHOLD || '60');
  const maxClonesPerRun = parseInt(process.env.MAX_CLONES_PER_RUN || '5', 10);
  const agingDate = new Date(Date.now() - thresholdMinutes * 60 * 1000);

  logger.cron(`Starting auto-clone cycle (threshold: ${thresholdMinutes} minute(s), cutoff: ${agingDate.toISOString()})`);

  const report = {
    thresholdMinutes,
    agingCutoffDate: agingDate.toISOString(),
    timestamp: new Date().toISOString(),
    clonesFinalized: 0,
    clonesInitiated: 0,
    deadVideosFound: 0,
    logs: [],
  };

  const addLog = (msg) => {
    report.logs.push(msg);
    logger.info(msg);
  };

  try {
    // -------------------------------------------------------------
    // PHASE 1: Finalize Any In-Flight Remote Upload Clones
    // -------------------------------------------------------------
    logger.cron('Phase 1: Checking for in-flight pending remote uploads...');
    const pendingVideos = await Video.find({
      pendingRemoteUploadId: { $exists: true, $ne: null },
    }).limit(10);

    if (pendingVideos.length === 0) {
      addLog('Phase 1: No pending in-flight uploads found.');
    } else {
      addLog(`Phase 1: Found ${pendingVideos.length} pending uploads to check.`);
    }

    for (const video of pendingVideos) {
      const uploadId = video.pendingRemoteUploadId;
      addLog(`Checking status for video "${video.title}" (${video._id}) with uploadId: ${uploadId}`);

      try {
        const statusData = await checkRemoteCloneStatus(uploadId);
        const resolvedLinkId = statusData.linkId || statusData.linkid || statusData.newFileId;

        if (statusData.status === 'finished' && resolvedLinkId) {
          const oldFileId = video.fileId;
          const newFileId = resolvedLinkId;
          const newUrl = `https://streamtape.com/v/${newFileId}/`;

          // Update MongoDB record
          video.fileId = newFileId;
          video.streamtapeUrl = newUrl;
          video.lastRefreshedAt = new Date();
          video.pendingRemoteUploadId = null;
          video.streamtapeStatus = 'active';
          await video.save();

          report.clonesFinalized += 1;
          logger.success(`Finalized in-flight clone for "${video.title}": ${oldFileId} -> ${newFileId}`);
          addLog(`Video "${video.title}" updated with new fileId: ${newFileId}`);

          // Invalidate Redis caches for Aurahub
          try {
            await invalidateVideoCaches(video._id.toString());
            addLog(`Invalidated Redis cache for video ${video._id}`);
          } catch (cacheErr) {
            logger.warn(`Redis cache invalidation warning for ${video._id}:`, cacheErr.message);
          }

          // Delete old video from Streamtape to conserve account storage
          if (oldFileId && oldFileId !== newFileId) {
            try {
              await deleteStreamtapeFile(oldFileId);
              logger.success(`Deleted old Streamtape file ${oldFileId}`);
              addLog(`Deleted old Streamtape file ${oldFileId}`);
            } catch (delErr) {
              logger.warn(`Could not delete old file ${oldFileId}:`, delErr.message);
              addLog(`Warning: old file ${oldFileId} deletion failed: ${delErr.message}`);
            }
          }
        } else if (statusData.status === 'error') {
          logger.error(`Remote clone failed for video "${video.title}" (${uploadId}): ${statusData.error}`);
          addLog(`Remote clone failed for "${video.title}". Resetting pending status.`);
          video.pendingRemoteUploadId = null;
          await video.save();
        } else {
          addLog(`Remote clone for "${video.title}" still processing (${statusData.status})...`);
        }
      } catch (err) {
        logger.error(`Error checking upload ${uploadId}:`, err.message);
        addLog(`Error checking upload ${uploadId}: ${err.message}`);
      }
    }

    // -------------------------------------------------------------
    // PHASE 2: Identify and Start Cloning for Aging Videos
    // -------------------------------------------------------------
    logger.cron(`Phase 2: Scanning for videos older than ${thresholdMinutes} minute(s) (max batch: ${maxClonesPerRun})...`);

    const agingVideos = await Video.find({
      streamtapeStatus: { $ne: 'dead' },
      pendingRemoteUploadId: null,
      $or: [
        { lastRefreshedAt: { $lt: agingDate } },
        { lastRefreshedAt: { $exists: false } },
        { lastRefreshedAt: null },
      ],
    })
      .sort({ lastRefreshedAt: 1, createdAt: 1 })
      .limit(maxClonesPerRun);

    if (agingVideos.length === 0) {
      addLog(`Phase 2: No videos older than ${thresholdMinutes} minute(s) require cloning.`);
    } else {
      addLog(`Phase 2: Found ${agingVideos.length} candidate videos for auto-cloning.`);
    }

    for (const video of agingVideos) {
      const fileId = video.fileId;
      if (!fileId) {
        addLog(`Skipping video "${video.title}" (${video._id}): no fileId found.`);
        continue;
      }

      // Direct Streamtape URL
      const sourceUrl = video.streamtapeUrl || `https://streamtape.com/v/${fileId}`;
      addLog(`Initiating direct remote clone for "${video.title}" from: ${sourceUrl}`);

      try {
        const cloneResult = await startRemoteClone(sourceUrl);

        if (!cloneResult.ok) {
          logger.warn(`Remote clone failed for "${video.title}": ${cloneResult.error}`);
          addLog(`Warning: Remote clone failed for "${video.title}": ${cloneResult.error}`);

          const lowerErr = (cloneResult.error || '').toLowerCase();
          if (lowerErr.includes('not found') || lowerErr.includes('deleted') || lowerErr.includes('404')) {
            video.streamtapeStatus = 'dead';
            await video.save();
            report.deadVideosFound += 1;
            addLog(`Marked video "${video.title}" as dead in DB.`);
          }
          continue;
        }

        // Streamtape internal clones return linkId immediately
        if (cloneResult.linkId) {
          const oldFileId = video.fileId;
          const newFileId = cloneResult.linkId;
          const newUrl = `https://streamtape.com/v/${newFileId}/`;

          video.fileId = newFileId;
          video.streamtapeUrl = newUrl;
          video.lastRefreshedAt = new Date();
          video.pendingRemoteUploadId = null;
          video.streamtapeStatus = 'active';
          await video.save();

          report.clonesFinalized += 1;
          logger.success(`Instant clone finalized for "${video.title}": ${oldFileId} -> ${newFileId}`);
          addLog(`Instant clone finalized for "${video.title}" with new fileId: ${newFileId}`);

          // Invalidate Redis cache
          try {
            await invalidateVideoCaches(video._id.toString());
            addLog(`Invalidated Redis cache for video ${video._id}`);
          } catch (cacheErr) {
            logger.warn(`Redis cache invalidation warning for ${video._id}:`, cacheErr.message);
          }

          // Delete old video from Streamtape
          if (oldFileId && oldFileId !== newFileId) {
            try {
              await deleteStreamtapeFile(oldFileId);
              logger.success(`Deleted old Streamtape file ${oldFileId}`);
              addLog(`Deleted old Streamtape file ${oldFileId}`);
            } catch (delErr) {
              logger.warn(`Could not delete old file ${oldFileId}:`, delErr.message);
            }
          }
        } else if (cloneResult.remoteId) {
          video.pendingRemoteUploadId = cloneResult.remoteId;
          await video.save();
          report.clonesInitiated += 1;
          logger.success(`Queued remote clone for "${video.title}"! Upload ID: ${cloneResult.remoteId}`);
          addLog(`Successfully queued remote clone with upload ID: ${cloneResult.remoteId}`);
        }
      } catch (cloneErr) {
        logger.error(`Error processing candidate "${video.title}":`, cloneErr.message);
        addLog(`Error processing candidate "${video.title}": ${cloneErr.message}`);
      }
    }

    report.durationMs = Date.now() - startTime;
    logger.cron(`Cycle completed in ${report.durationMs}ms: ${report.clonesFinalized} finalized, ${report.clonesInitiated} initiated, ${report.deadVideosFound} dead.`);

    return {
      success: true,
      ...report,
    };
  } catch (error) {
    logger.error('Auto-clone cycle encountered a fatal error:', error.message);
    const durationMs = Date.now() - startTime;

    return {
      success: false,
      error: error.message,
      durationMs,
      ...report,
    };
  }
}
