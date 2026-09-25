import axios from 'axios';

const AURA_API_BASE_URL = process.env.AURA_API_BASE_URL || 'https://aurahub-api.ashwathama249.workers.dev';
const UPLOAD_FOLDER_ID = process.env.UPLOAD_FOLDER_ID;

const BROWSER_USER_AGENT =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36';

/**
 * Resolves a Streamtape fileId to a direct streaming MP4 link.
 */
export async function resolveStreamtapeUrl(fileId) {
  if (!fileId || typeof fileId !== 'string') {
    return { ok: false, error: 'Invalid fileId' };
  }

  const cleanId = fileId.trim();

  try {
    const embedUrl = `https://streamtape.com/e/${cleanId}`;
    const response = await fetch(embedUrl, {
      headers: { 'User-Agent': BROWSER_USER_AGENT },
      cache: 'no-store',
      signal: AbortSignal.timeout(10000),
    });

    if (!response.ok) {
      return { ok: false, status: response.status, error: `Streamtape returned status ${response.status}` };
    }

    const html = await response.text();

    if (
      html.includes('File not found') ||
      html.includes('Video has been removed') ||
      html.includes('File was deleted')
    ) {
      return { ok: false, status: 404, error: 'File deleted on Streamtape' };
    }

    const robotMatch = html.match(
      /document\.getElementById\(['"]robotlink['"]\)\.innerHTML\s*=\s*(.+?);/
    );

    if (!robotMatch || !robotMatch[1]) {
      return { ok: false, status: 502, error: 'Could not find robotlink on Streamtape' };
    }

    const rawExpr = robotMatch[1].trim();
    const evaluated = Function(`"use strict"; return (${rawExpr});`)();
    const streamUrl = evaluated.startsWith('//') ? `https:${evaluated}` : evaluated;

    if (!streamUrl || !streamUrl.startsWith('http')) {
      return { ok: false, status: 500, error: 'Parsed invalid stream URL' };
    }

    return { ok: true, streamUrl };
  } catch (err) {
    return { ok: false, status: 500, error: err.message };
  }
}

/**
 * Starts a Streamtape remote upload from direct stream URL into the Aurahub folder.
 */
export async function startRemoteClone(streamUrl) {
  try {
    const response = await axios.get(`${AURA_API_BASE_URL}/remote/add`, {
      params: { url: streamUrl, folder: UPLOAD_FOLDER_ID },
      timeout: 15000,
    });

    const remoteId = response.data?.result?.id || response.data?.id;
    if (!remoteId) {
      return { ok: false, error: 'No remote upload ID returned' };
    }

    return { ok: true, remoteId };
  } catch (err) {
    return { ok: false, error: err.response?.data?.message || err.message };
  }
}

/**
 * Checks the status of an ongoing remote upload on Streamtape.
 */
export async function checkRemoteCloneStatus(remoteId) {
  try {
    const response = await axios.get(`${AURA_API_BASE_URL}/remote/status`, {
      params: { id: remoteId },
      timeout: 15000,
    });

    const data = response.data?.[remoteId] || response.data?.result?.[remoteId];

    if (!data) {
      return { status: 'unknown' };
    }

    if (data.status === 'finished' && data.linkid) {
      return { status: 'finished', linkid: data.linkid };
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
    return { status: 'error', error: err.message };
  }
}

/**
 * Deletes the old file from Streamtape via the proxy worker.
 */
export async function deleteStreamtapeFile(fileId) {
  if (!fileId) return false;
  try {
    await axios.delete(`${AURA_API_BASE_URL}/fs/files/delete/${fileId}`, {
      timeout: 10000,
    });
    return true;
  } catch (err) {
    console.warn(`[Streamtape] Delete old file ${fileId} warning:`, err.message);
    return false;
  }
}
