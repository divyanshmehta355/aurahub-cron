# Aurahub Auto-Clone Service (Go)

A high-performance, lightweight Go microservice that automatically clones Streamtape videos approaching expiration before Streamtape's 90-day inactivity purge limit.

Designed for native deployment on **Render** (without Docker) with ultra-low memory consumption (~15MB RAM) and zero cold-sleep memory bloat.

---

## Features

- **Blazing Fast & Ultra-Low Memory**: Written in idiomatic Go with standard library `net/http` and official drivers. Consumes ~15MB RAM on Render.
- **Native OS Sockets & SRV Resolution**: Connects to MongoDB Atlas (`mongodb+srv://`) and Redis directly without serverless socket limitations.
- **Resilient 2-Phase Execution**:
  - **Phase 1**: Checks and finalizes in-flight uploads (`pendingRemoteUploadId`), updates MongoDB, invalidates Redis caches, and deletes old files.
  - **Phase 2**: Detects aging videos older than `AGING_MINUTES_THRESHOLD` and queues or completes remote clones.
- **Cache Synchronization**: Automatically invalidates Aurahub Redis video feeds (`videos_*`) and individual video keys.
- **Render Ready**: Native Go environment (no Docker container required) with zero-downtime health checks at `GET /health`.

---

## Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/` | Service status, runtime, uptime, and thresholds |
| `GET` | `/health` | Render liveness health check (returns `{"status":"ok"}`) |
| `GET / POST` | `/api/cron/auto-clone?key=<SECRET>` | Secured trigger endpoint (also accepts `x-cron-key` header) |

---

## Running Locally

Run directly through your terminal:
```bash
go run .
```
*(Loads variables from `.env` automatically on startup).*

Test the trigger:
```
http://localhost:4000/api/cron/auto-clone?key=YOUR_CRON_SECRET
```

---

## Deploying to Render (Native Go, No Docker)

1. Push this repository to GitHub.
2. Log into the [Render Dashboard](https://dashboard.render.com).
3. Click **New +** -> **Web Service**.
4. Connect your repository.
5. Configure the service:
   - **Name**: `aurahub-cron`
   - **Language / Environment**: `Go`
   - **Branch**: `master` (or `main`)
   - **Build Command**: `go build -o server .`
   - **Start Command**: `./server`
   - **Plan**: `Free`
6. Under **Environment Variables**, add:
   - `MONGO_URI`: `mongodb+srv://...`
   - `REDIS_URL`: `redis://...`
   - `AURA_API_BASE_URL`: `https://aurahub-api-hono.ashwathama249.workers.dev`
   - `UPLOAD_FOLDER_ID`: `QU3yuiRZZFw`
   - `CRON_SECRET`: your secret token
   - `AGING_MINUTES_THRESHOLD`: `60` (or `108000` for 75 days)
   - `MAX_CLONES_PER_RUN`: `5`
7. Click **Create Web Service**. Render compiles the Go binary and provides your public URL (e.g., `https://aurahub-cron.onrender.com`).

---

## Setting Up Scheduled Invocations

You can trigger the service on a schedule using [cron-job.org](https://cron-job.org/en/):
- **URL**: `https://aurahub-cron.onrender.com/api/cron/auto-clone?key=YOUR_CRON_SECRET`
- **Schedule**: Once a day (e.g. 03:00 UTC) or once a week.
- **Timeout**: `60 seconds`.
