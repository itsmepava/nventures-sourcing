# nVentures Workspace — frontend

A standalone web app: plain HTML, CSS and native JavaScript ES modules, with no build step, framework or third-party assets. It talks to the Go backend **only** through the JSON API under `/api`.

```
web/src/
  index.html          static shell (no server templating)
  assets/             app.css, theme.js (no-flash theme), brand images
  js/core.js          DOM builder, API client, theme, dialogs, roles (can/need)
  js/components.js    tables, job progress, workflow, notes, reply drafts
  js/views/*.js       one module per page (auth, dashboard, queue, inbox, …)
  js/main.js          sign-in gate, shell, router, command palette, shortcuts
web/embed.go          the only Go file: embeds src/ into the binary
```

- **Production:** `go build ./cmd/nventures` embeds `web/src`, so the app starts with one command.
- **Development:** run the backend with `NV_WEB_DIR=web/src` to serve these files from disk; edit and reload the browser, no rebuild needed.
- **Rules:** insert untrusted text only through `h()` (textContent), never `innerHTML`; set styles via the `style` attribute in `h()` (CSSOM, allowed by the strict CSP); every mutating request sends `X-NV-Request: 1` (done by `api()`).
