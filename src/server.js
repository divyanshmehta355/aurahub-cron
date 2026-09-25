import 'dotenv/config';
import express from 'express';
import { connectDB } from './config/db.js';
import { runAutoCloneCycle } from './services/autoCloneService.js';
import { logger } from './lib/logger.js';

const app = express();
const PORT = process.env.PORT || 4000;
const CRON_SECRET_KEY = process.env.CRON_SECRET_KEY || process.env.CRON_SECRET || 'aurahub_cron_secret_key_2026';

app.use(express.json());

// Request logger middleware (logs all HTTP hits in standard format)
app.use((req, res, next) => {
  const start = Date.now();
  res.on('finish', () => {
    const duration = Date.now() - start;
    logger.http(`${req.method} ${req.originalUrl} ${res.statusCode} - ${duration}ms`);
  });
  next();
});

// Middleware to authenticate external cron caller
function authenticateCron(req, res, next) {
  const authHeader = req.headers['x-cron-key'];
  const queryKey = req.query.key;

  if ((authHeader && authHeader === CRON_SECRET_KEY) || (queryKey && queryKey === CRON_SECRET_KEY)) {
    return next();
  }

  logger.warn(`Unauthorized access attempt from ${req.ip} to ${req.path}`);
  return res.status(401).json({
    success: false,
    error: 'Unauthorized: Invalid or missing x-cron-key header or key query parameter',
  });
}

// -------------------------------------------------------------
// Public Health Check Endpoint
// -------------------------------------------------------------
app.get('/', (req, res) => {
  const thresholdMinutes = parseFloat(process.env.AGING_MINUTES_THRESHOLD || '60');

  res.status(200).json({
    service: 'aurahub-cron',
    status: 'online',
    timestamp: new Date().toISOString(),
    agingMinutesThreshold: thresholdMinutes,
    maxClonesPerRun: parseInt(process.env.MAX_CLONES_PER_RUN || '5', 10),
    endpoints: {
      health: 'GET /health',
      triggerAutoClone: 'POST or GET /api/cron/auto-clone?key=YOUR_KEY',
    },
  });
});

app.get('/health', (req, res) => {
  res.status(200).json({ status: 'ok', uptime: process.uptime() });
});

// -------------------------------------------------------------
// Protected Cron Trigger Endpoint
// Called by cron-job.org or manual POST / GET
// -------------------------------------------------------------
app.post('/api/cron/auto-clone', authenticateCron, async (req, res) => {
  logger.cron(`Trigger received from IP: ${req.ip}`);

  try {
    const result = await runAutoCloneCycle();
    return res.status(result.success ? 200 : 500).json(result);
  } catch (err) {
    logger.error('Unexpected error in cron trigger route:', err.message);
    return res.status(500).json({
      success: false,
      error: err.message,
    });
  }
});

app.get('/api/cron/auto-clone', authenticateCron, async (req, res) => {
  logger.cron(`GET trigger received from IP: ${req.ip}`);
  try {
    const result = await runAutoCloneCycle();
    return res.status(result.success ? 200 : 500).json(result);
  } catch (err) {
    logger.error('Unexpected error in cron trigger route:', err.message);
    return res.status(500).json({
      success: false,
      error: err.message,
    });
  }
});

// Start Server
app.listen(PORT, async () => {
  logger.info('===================================================');
  logger.info(`  Aurahub Auto-Clone Service listening on port ${PORT}`);
  logger.info('===================================================');

  try {
    await connectDB();
  } catch (err) {
    logger.error('Initial database connection error:', err.message);
  }
});
