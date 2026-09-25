import { createClient } from 'redis';

let client = null;
let connectingPromise = null;

export async function getRedis() {
  const redisUrl = process.env.REDIS_URL;
  if (!redisUrl) {
    return null;
  }

  if (client && client.isOpen) {
    return client;
  }

  if (!client) {
    client = createClient({
      url: redisUrl,
      socket: {
        reconnectStrategy: (retries) => (retries > 5 ? new Error('Redis max retries') : Math.min(retries * 200, 2000)),
      },
    });

    client.on('error', (err) => {
      if (err?.message?.includes('Socket closed unexpectedly')) return;
      console.warn('[Redis] Client Warning:', err.message);
    });
  }

  if (!connectingPromise) {
    connectingPromise = client.connect().catch((err) => {
      connectingPromise = null;
      throw err;
    }).finally(() => {
      connectingPromise = null;
    });
  }

  await connectingPromise;
  return client;
}

/**
 * Scans and evicts keys matching wildcard pattern across Redis
 */
export async function delPattern(pattern) {
  try {
    const c = await getRedis();
    if (!c) return 0;

    const keysToDelete = [];
    for await (const key of c.scanIterator({ MATCH: pattern, COUNT: 100 })) {
      keysToDelete.push(key);
    }

    if (keysToDelete.length > 0) {
      for (let i = 0; i < keysToDelete.length; i += 200) {
        await c.del(keysToDelete.slice(i, i + 200));
      }
      return keysToDelete.length;
    }
    return 0;
  } catch (err) {
    console.warn('[Redis] delPattern error:', err.message);
    return 0;
  }
}

/**
 * Invalidates all feed caches and video-specific caches in Aurahub Redis
 */
export async function invalidateVideoCaches(videoIds) {
  const ids = Array.isArray(videoIds) ? videoIds : (videoIds ? [videoIds] : []);
  try {
    const c = await getRedis();
    if (!c) return;

    // Invalidate feed listings across categories, sort orders, and pagination
    await delPattern('videos_*');
    await delPattern('suggestions:*');
    await delPattern('recommendations:*');
    await delPattern('search:*');

    // Invalidate individual video caches
    for (const id of ids) {
      const idStr = id?.toString?.() || String(id);
      if (idStr) {
        await c.del(`video:${idStr}`);
        await c.del(`stream:${idStr}`);
        await delPattern(`*${idStr}*`);
      }
    }
  } catch (err) {
    console.warn('[Redis] Invalidate error:', err.message);
  }
}
