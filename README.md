# Aurahub Auto-Clone Service (Node.js)

A lightweight, standalone Node.js microservice that automatically clones Streamtape videos approaching expiration. This prevents Streamtape from permanently purging videos after 90 days.

---

## Architecture & Features

- **Pure Auto-Clone**: Clones aging videos via Streamtape's Remote Upload API (`/remote/add`).
- **Resilient & Non-Blocking**: Uses `pendingRemoteUploadId` so large video transfers never cause HTTP timeouts. Initiated in one cycle, finalized in the next.
- **Cache Synchronization**: Automatically invalidates Aurahub's Redis video feeds (`videos_*`) and individual video keys when a file ID is refreshed.
- **Clean Cleanup**: Deletes the old expiring file from Streamtape once the new clone is confirmed.
- **Regular Console Logging**: Beautiful, colorized, timestamped stdout/stderr logging (`[INFO]`, `[SUCCESS]`, `[WARN]`, `[ERROR]`, `[CRON]`, `[HTTP]`) for terminal and Render logs tab.
- **Zero Database Pollution**: Does not store logs in MongoDB. Only modifies the `Video` collection when updating refreshed video IDs.
- **Render Ready**: Includes a `GET /health` endpoint for Render zero-downtime health checks.

---

## Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/` | Service status, runtime, and configured thresholds |
| `GET` | `/health` | Render liveness check (returns `{"status":"ok"}`) |
| `GET / POST` | `/api/cron/auto-clone?key=<CRON_SECRET>` | Secured cron trigger endpoint called by **cron-job.org** |

---

## Local Setup

1. Check `.env`:
   ```bash
   cp .env.example .env
   ```
2. Start the service in dev mode:
   ```bash
   npm run dev
   ```
   *(Uses `node --watch src/server.js` so it automatically reloads on code edits).*

3. Test trigger in browser or curl:
   ```
   http://localhost:4000/api/cron/auto-clone?key=aurahub_cron_secret_key_2026
   ```

---

## Deploying to Render (Free Web Service)

1. Push this repository to GitHub (e.g. as `aurahub-cron`).
2. Log into [Render Dashboard](https://dashboard.render.com).
3. Click **New +** -> **Web Service**.
4. Connect your `aurahub-cron` repository.
5. Configure the service:
   - **Name**: `aurahub-cron`
   - **Environment**: `Node`
   - **Build Command**: `npm install`
   - **Start Command**: `npm start`
   - **Plan**: `Free`
6. Under **Environment Variables**, add:
   - `MONGO_URI`
   - `REDIS_URL`
   - `AURA_API_BASE_URL` (`https://aurahub-api.ashwathama249.workers.dev`)
   - `UPLOAD_FOLDER_ID`
   - `CRON_SECRET`
   - `AGING_MINUTES_THRESHOLD` (`75`)
   - `MAX_CLONES_PER_RUN` (`5`)
7. Click **Create Web Service**. Render provides your public URL (e.g., `https://aurahub-cron.onrender.com`).

---

## Setting up cron-job.org

1. Log in to [cron-job.org](https://cron-job.org/en/).
2. Click **Create Cronjob**.
3. Fill in:
   - **Title**: `Aurahub Streamtape Auto-Clone`
   - **URL**: `https://aurahub-cron.onrender.com/api/cron/auto-clone?key=YOUR_CRON_SECRET`
   - **Schedule**: Run **Once a Day** (e.g. at 03:00 UTC) or **Once a Week**.
   - **Request Timeout**: `60 seconds`.
4. Save the cron job!
