import axios from 'axios';
import { logger } from './logger.js';

function getApiBaseUrl() {
  return process.env.AURA_API_BASE_URL || 'https://aurahub-api-hono.ashwathama249.workers.dev';
}

function getUploadFolderId() {
  return process.env.UPLOAD_FOLDER_ID || 'QU3yuiRZZFw';
}

/**
 * Starts a Streamtape remote upload directly from any Streamtape URL (e.g. https://streamtape.com/v/ID or /e/ID).
 * Conforms to Cloudflare Worker OpenAPI spec:
 * GET /remote/add?url={url}&folder={folder}&name={name}
 */
export async function startRemoteClone(videoUrl, customFolder = null) {
  if (!videoUrl) {
    return { ok: false, error: 'videoUrl is required' };
  }

  const folder = customFolder || getUploadFolderId();
  if (!folder) {
    logger.error('[Streamtape] UPLOAD_FOLDER_ID is missing in environment variables');
    return { ok: false, error: 'UPLOAD_FOLDER_ID is required' };
  }

  const baseUrl = getApiBaseUrl();

  try {
    const response = await axios.get(`${baseUrl}/remote/add`, {
      params: {
        url: videoUrl,
        folder: folder,
      },
      timeout: 20000,
    });

    const data = response.data;
    const remoteId = data?.result?.id || data?.id;
    const linkId = data?.result?.linkid || data?.linkid;
    const link = data?.result?.link || data?.link;

    if (!remoteId && !linkId) {
      return { ok: false, error: data?.msg || data?.message || 'No upload ID or link ID returned' };
    }

    return {
      ok: true,
      remoteId,
      linkId,
      link,
    };
  } catch (err) {
    const status = err.response?.status;
    const errData = err.response?.data;
    const msg = errData?.error?.message || errData?.message || errData?.msg || err.message;
    logger.error(`[Streamtape] /remote/add failed (status ${status}):`, typeof msg === 'object' ? JSON.stringify(msg) : msg);
    return { ok: false, status, error: typeof msg === 'object' ? JSON.stringify(msg) : msg };
  }
}

/**
 * Checks the status of an ongoing remote upload on Streamtape.
 * Conforms to Cloudflare Worker OpenAPI spec:
 * GET /remote/status?id={id}&limit={limit}
 */
export async function checkRemoteCloneStatus(remoteId) {
  if (!remoteId) {
    return { status: 'error', error: 'remoteId is required' };
  }

  const baseUrl = getApiBaseUrl();

  try {
    const response = await axios.get(`${baseUrl}/remote/status`, {
      params: { id: remoteId },
      timeout: 15000,
    });

    const data = response.data?.[remoteId] || response.data?.result?.[remoteId];

    if (!data) {
      return { status: 'unknown' };
    }

    if ((data.status === 'finished' || data.status === 'completed') && (data.linkid || data.linkId)) {
      return {
        status: 'finished',
        linkId: data.linkid || data.linkId,
      };
    }

    if (data.status === 'error') {
      return { status: 'error', error: data.error_message || 'Remote upload error' };
    }

    return {
      status: data.status || 'downloading',
      bytesLoaded: data.bytes_loaded,
      bytesTotal: data.bytes_total,
    };
  } catch (err) {
    const status = err.response?.status;
    const errData = err.response?.data;
    const msg = errData?.error?.message || errData?.message || err.message;
    logger.error(`[Streamtape] /remote/status failed (status ${status}):`, msg);
    return { status: 'error', error: msg };
  }
}

/**
 * Deletes the old file from Streamtape via the proxy worker.
 * Conforms to Cloudflare Worker OpenAPI spec:
 * DELETE /fs/files/delete/{file_id}
 */
export async function deleteStreamtapeFile(fileId) {
  if (!fileId) return false;
  const baseUrl = getApiBaseUrl();
  try {
    await axios.delete(`${baseUrl}/fs/files/delete/${fileId}`, {
      timeout: 10000,
    });
    return true;
  } catch (err) {
    logger.warn(`[Streamtape] Delete file ${fileId} warning:`, err.response?.data?.message || err.message);
    return false;
  }
}
