import Video from '../models/Video.js';
import {
  resolveStreamtapeUrl,
  startRemoteClone,
  checkRemoteCloneStatus,
  deleteStreamtapeFile,
} from '../lib/streamtape.js';
import { invalidateVideoCaches } from '../config/redis.js';

/**
 * Executes a full Auto-Clone cycle:
 * 1. Finalize in-flight remote uploads.
 * 2. Identify aging videos (> 75 days) and initiate remote clones.
 */
export async function runAutoCloneCycle() {
  const thresholdDays = parseInt(process.env.AGING_DAYS_THRESHOLD || '75', 10);
  const maxClonesPerRun = parseInt(process.env.MAX_CLONES_PER_RUN || '5', 10);

  const report = {
    timestamp: new Date().toISOString(),
    clonesFinalized: 0,
    clonesInitiated: 0,
    deadVideosFound: 0,
    logs: [],
  };

  // -------------------------------------------------------------
  // PHASE 1: Finalize Any In-Flight Remote Upload Clones
  // -------------------------------------------------------------
  const pendingVideos = await Video.find({
    pendingRemoteUploadId: { $exists: true, $ne: null },
  }).limit(10);

  for (const vid of pendingVideos) {
    try {
      const statusRes = await checkRemoteCloneStatus(vid.pendingRemoteUploadId);

      if (statusRes.status === 'finished' && statusRes.linkid) {
        const oldFileId = vid.fileId;
        const newFileId = statusRes.linkid;

        vid.fileId = newFileId;
        vid.lastRefreshedAt = new Date();
        vid.pendingRemoteUploadId = undefined;
        vid.streamtapeStatus = 'active';
        await vid.save();

        await invalidateVideoCaches(vid._id);
        await deleteStreamtapeFile(oldFileId);

        report.clonesFinalized++;
        report.logs.push(`Finalized clone for "${vid.title}" (${vid._id}): ${oldFileId} -> ${newFileId}`);
      } else if (statusRes.status === 'error') {
        report.logs.push(`Remote upload error for "${vid.title}": ${statusRes.error}. Resetting pending ID.`);
        vid.pendingRemoteUploadId = undefined;
        await vid.save();
      } else {
        report.logs.push(`Clone in progress for "${vid.title}": status=${statusRes.status}`);
      }
    } catch (err) {
      report.logs.push(`Error checking in-flight clone for "${vid.title}": ${err.message}`);
    }
  }

  // -------------------------------------------------------------
  // PHASE 2: Trigger Clones for Aging Videos (> thresholdDays)
  // -------------------------------------------------------------
  const agingDate = new Date(Date.now() - thresholdDays * 24 * 60 * 60 * 1000);

  const agingVideos = await Video.find({
    $or: [
      { lastRefreshedAt: { $lt: agingDate } },
      { lastRefreshedAt: { $exists: false }, createdAt: { $lt: agingDate } },
    ],
    pendingRemoteUploadId: { $in: [null, undefined] },
    streamtapeStatus: { $ne: 'dead' },
  }).limit(maxClonesPerRun);

  for (const vid of agingVideos) {
    try {
      // 1. Resolve direct stream link
      const streamInfo = await resolveStreamtapeUrl(vid.fileId);

      if (!streamInfo.ok || !streamInfo.streamUrl) {
        if (streamInfo.status === 404) {
          vid.streamtapeStatus = 'dead';
          await vid.save();
          report.deadVideosFound++;
          report.logs.push(`Video "${vid.title}" (${vid.fileId}) is 404/deleted on Streamtape.`);
        }
        continue;
      }

      // 2. Start remote upload to clone the file
      const cloneRes = await startRemoteClone(streamInfo.streamUrl);

      if (cloneRes.ok && cloneRes.remoteId) {
        vid.pendingRemoteUploadId = cloneRes.remoteId;
        await vid.save();
        report.clonesInitiated++;
        report.logs.push(`Initiated remote clone for aging video "${vid.title}" (remoteId: ${cloneRes.remoteId})`);

        // Check if finished immediately (CDN internal transfer often finishes in 2-3s)
        await new Promise((r) => setTimeout(r, 2500));
        const quickStatus = await checkRemoteCloneStatus(cloneRes.remoteId);

        if (quickStatus.status === 'finished' && quickStatus.linkid) {
          const oldFileId = vid.fileId;
          vid.fileId = quickStatus.linkid;
          vid.lastRefreshedAt = new Date();
          vid.pendingRemoteUploadId = undefined;
          vid.streamtapeStatus = 'active';
          await vid.save();

          await invalidateVideoCaches(vid._id);
          await deleteStreamtapeFile(oldFileId);

          report.clonesFinalized++;
          report.logs.push(`Fast-completed clone for "${vid.title}": new fileId ${quickStatus.linkid}`);
        }
      } else {
        report.logs.push(`Failed to start clone for "${vid.title}": ${cloneRes.error}`);
      }
    } catch (err) {
      report.logs.push(`Error processing aging video "${vid.title}": ${err.message}`);
    }
  }

  return report;
}
