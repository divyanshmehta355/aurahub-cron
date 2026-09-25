import 'dotenv/config';
import express from 'express';
import { connectDB } from './config/db.js';
import { runAutoCloneCycle } from './services/autoCloneService.js';

const app = express();
const PORT = process.env.PORT || 4000;

app.use(express.json());

// 1. Healthcheck Endpoint (Zero downtime / keep-alive on Render)
app.get('/health', (req, res) => {
  res.status(200).json({
    status: 'ok',
    service: 'aurahub-cron',
    timestamp: new Date().toISOString(),
  });
});

app.get('/', (req, res) => {
  res.status(200).json({
    message: 'Aurahub Auto-Clone Cron Microservice is running.',
    endpoints: {
      health: 'GET /health',
      cronTrigger: 'GET or POST /api/cron/auto-clone?key=YOUR_CRON_SECRET',
    },
  });
});

// 2. Authorization Middleware for Cron Trigger
function checkCronAuth(req, res, next) {
  const cronSecret = process.env.CRON_SECRET;
  if (!cronSecret) {
    console.warn('[Security Warning] CRON_SECRET is not configured.');
    return next();
  }

  const key = req.query.key;
  const authHeader = req.headers['authorization'];
  const bearerToken = authHeader?.startsWith('Bearer ') ? authHeader.slice(7) : null;

  if (key === cronSecret || bearerToken === cronSecret) {
    return next();
  }

  return res.status(401).json({
    success: false,
    message: 'Unauthorized: Invalid or missing secret key.',
  });
}

// 3. Auto-Clone Trigger Endpoint (Called by cron-job.org)
async function handleAutoClone(req, res) {
  console.log(`[Cron] Auto-clone triggered at ${new Date().toISOString()}`);
  try {
    await connectDB();
    const report = await runAutoCloneCycle();

    return res.status(200).json({
      success: true,
      message: 'Auto-clone cycle executed successfully.',
      summary: {
        clonesFinalized: report.clonesFinalized,
        clonesInitiated: report.clonesInitiated,
        deadVideosFound: report.deadVideosFound,
      },
      ...report,
    });
  } catch (err) {
    console.error('[Cron] Auto-clone execution error:', err);
    return res.status(500).json({
      success: false,
      message: 'Failed to execute auto-clone cycle.',
      error: err.message,
    });
  }
}

app.get('/api/cron/auto-clone', checkCronAuth, handleAutoClone);
app.post('/api/cron/auto-clone', checkCronAuth, handleAutoClone);

// Start Server
app.listen(PORT, async () => {
  console.log(`===================================================`);
  console.log(`  Aurahub Auto-Clone Service listening on port ${PORT}`);
  console.log(`===================================================`);

  try {
    await connectDB();
  } catch (err) {
    console.error('[Database] Initial connection attempt error:', err.message);
  }
});
