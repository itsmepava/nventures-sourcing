# Local development now; Google Cloud readiness later

User clarification, 12 November 2026: develop and demonstrate the complete app on localhost, and most likely host on Google's servers later. Interpret this as Google Cloud Platform (GCP). Do not create billable resources or deploy during the local implementation.

Finish all local workflows before infrastructure work. Design the Go server to support configurable host/PORT, data directory, external base URL, OAuth redirects, trusted proxies, graceful shutdown, readiness/liveness, structured secret-safe logs and a build version. Local defaults remain loopback and a local SQLite file. Remote binding must be explicit and protected by authentication; do not turn off origin/host protections merely to make cloud deployment pass.

Keep storage and job leases behind small interfaces with real migrations. For a first single-instance hosted deployment, a Compute Engine VM with a persistent disk can retain the local SQLite/single-process model with fewer new managed services. This is an architectural option, not a claim it is always the cheapest service; price the actual region, uptime, disk, network, backups and operations before provisioning. Back up SQLite with its consistency rules, test restoration and define maintenance access.

Cloud Run is another option, but the writable container filesystem is in-memory/ephemeral and cannot be the production SQLite database. Do not deploy this design there with `./app.db` and assume data survives restart or multiple replicas. A Cloud Run design needs persistent external storage (typically a proper shared SQL database for concurrent app instances), explicit connection budgets and durable job execution/leases. Do not promise that background goroutines continue indefinitely outside a request or across instance shutdown. Additional managed services and persistence have operating costs; do not add them to the mandatory localhost stack.

Before internet exposure, implement approved team access/authentication, secure sessions/CSRF/origins, HTTPS/proxy settings, appropriate Google OAuth web consent/redirect configuration, secret injection, least-privilege service identity, durable audit and backups. Existing Desktop loopback OAuth config cannot simply be assumed valid for a hosted callback. Missing credentials/billing consent are external setup, not reasons to delay the local application.

Deliver an optional deployment runbook/container build after the application is working, plus a decision record comparing a single VM/persistent disk with stateless Cloud Run/external database. Do not add Terraform/Kubernetes/queues/Redis or purchase services simply for future readiness. User authorization to develop locally does not authorize deployment or cloud spending.

Primary references:

- Cloud Run port, filesystem, shutdown and lifecycle contract: https://docs.cloud.google.com/run/docs/container-contract
- Compute Engine persistent storage: https://docs.cloud.google.com/compute/docs/disks/persistent-disks
