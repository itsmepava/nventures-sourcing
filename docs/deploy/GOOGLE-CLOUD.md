# Future Google Cloud hosting — runbook and decision record

**Status: not deployed.** Nothing has been provisioned and no cloud charges were incurred. This document prepares a later decision; it does not authorise one.

## Decision record (draft)

**Context.** The app is a single Go process with a local SQLite database, background jobs that run for minutes, and a small team of users. Local use needs no cloud at all.

| | A. Compute Engine VM + persistent disk | B. Cloud Run + external database |
|---|---|---|
| App changes | None: same binary, same SQLite file on the persistent disk | Replace SQLite with a shared SQL database (e.g. Cloud SQL), move job execution to durable leases/queue, budget connections |
| Background jobs | Run in-process as today | Requests can end and instances can stop; long jobs need Cloud Run jobs or a queue — goroutines are not guaranteed to continue |
| Persistence | Persistent disk + snapshot schedule | Container filesystem is in-memory and lost on restart; must not hold `./app.db` |
| Operations | Patch the VM OS, manage the disk, snapshots | Fewer servers to patch; more managed services and their costs |
| Fit today | **Recommended first step** for a single instance | Only if many concurrent instances are needed |

**Proposed decision:** start with option A (one small VM, persistent disk, scheduled snapshots, HTTPS via a managed load balancer or a reverse proxy with a certificate), behind the app's access token and Google sign-in at the proxy. Price the region, machine type, uptime, disk, snapshots and egress before provisioning; do not assume A is always cheapest.

## What the app already supports for hosting

- `NV_HOST` / `NV_PORT` (or `PORT`) binding; non-loopback binding is **refused** unless `NV_ACCESS_TOKEN` (≥ 24 characters) is set, which then requires sign-in (HttpOnly, SameSite=Strict cookie; `Secure` when `NV_BASE_URL` is https).
- `NV_BASE_URL` for the external URL: used for the OAuth redirect and as an allowed `Host`; Host/Origin checks stay on (DNS-rebinding and cross-site protection are never disabled).
- `NV_DATA_DIR` for the persistent disk mount; `/health` (liveness) and `/readyz` (database readiness); `NV_LOG_JSON=1` structured logs without secrets or email bodies; graceful shutdown on SIGTERM with job cancellation and honest *interrupted* states; build version in `/health`.
- An instance lease in SQLite prevents two processes using one data directory.

## Before any internet exposure (required)

1. Team access: put Identity-Aware Proxy or an authenticating reverse proxy in front, in addition to `NV_ACCESS_TOKEN`.
2. HTTPS only; set `NV_BASE_URL=https://…`.
3. Google OAuth: create a **Web application** OAuth client with redirect `https://<host>/oauth/callback` (the local Desktop client is not valid for a hosted callback). Re-run consent.
4. Secrets: provide `NV_ACCESS_TOKEN` and the client file through Secret Manager or the VM's protected environment, never in the image or Git.
5. Service identity: least privilege; the VM needs no broad Google API roles because the app uses the user's OAuth consent.
6. Backups: scheduled disk snapshots **and** a nightly `nventures backup` (consistent `VACUUM INTO`) copied off the VM; test a restore.
7. Maintenance access via IAP SSH only; no public SSH.

## Option A outline (do not run until approved)

```bash
# Build the Linux binary locally
VERSION=0.3.0-alpha.1 sh scripts/build_release.sh
# On the VM (Debian), as a dedicated user:
sudo useradd --system --home /var/lib/nventures nventures
sudo install -o nventures -m 0750 -d /var/lib/nventures
sudo install -m 0755 nventures-0.3.0-alpha.1-linux-amd64 /usr/local/bin/nventures
# /etc/systemd/system/nventures.service
# [Service]
# User=nventures
# Environment=APP_MODE=live NV_DATA_DIR=/var/lib/nventures NV_HOST=127.0.0.1 NV_PORT=8765 NV_BASE_URL=https://sourcing.example NV_NO_BROWSER=1 NV_LOG_JSON=1
# EnvironmentFile=/etc/nventures/secrets.env   # NV_ACCESS_TOKEN=..., mode 0600
# ExecStart=/usr/local/bin/nventures serve
# Restart=on-failure
```

Keep the app bound to 127.0.0.1 behind the HTTPS proxy on the same VM; the proxy forwards the original `Host` that matches `NV_BASE_URL`.

## Container (optional)

`deploy/Dockerfile` builds a static image. It is provided for option A on Container-Optimized OS or for experiments; it has **not** been built or run here (no container tooling is installed), and it must not be used on Cloud Run with SQLite.

References: Cloud Run container contract (filesystem, shutdown) https://docs.cloud.google.com/run/docs/container-contract · Compute Engine persistent disks https://docs.cloud.google.com/compute/docs/disks/persistent-disks
