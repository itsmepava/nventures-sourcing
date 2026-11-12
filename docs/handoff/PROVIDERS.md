# Replacing Tavily and OpenRouter

## Decision

Default operation uses **no paid search or hosted model API**. Replace Tavily with Go-native, first-party discovery and replace OpenRouter extraction with deterministic parsing plus optional local inference. Preserve paid adapters only as explicit compatibility options, never automatic fallback.

This reduces recurring provider charges, external data transmission and mandatory services. It does not guarantee the same global-web coverage or research quality as a paid search index and hosted LLM. Treat coverage and confidence as observable product data. Internet access to Gmail, Sheets and company sites is still needed for live jobs; local model inference also consumes local hardware resources.

## Tavily replacement: direct discovery

1. Begin with each configured partner's official website, portfolio/investments pages, robots.txt, allowed sitemap URLs and RSS/Atom feeds. Follow bounded same-site navigation to portfolio listings; extract official company links and canonical identities.
2. Fetch discovered company home/about/product/team/news pages, then independently evidenced funding announcements from reachable linked sources. Retain timestamp, URL, normalized content and exact excerpt. Extract structured JSON-LD/schema.org metadata and conventional contact/business sections before heuristics.
3. Respect robots and source access restrictions, per-host rate limits, timeouts, response/body/page/depth bounds, MIME types, redirects and cancellation. Cache successful fetches by normalized URL/content hash with freshness and conditional requests. Do not circumvent logins, CAPTCHAs or bot protection.
4. Prevent SSRF: reject private/loopback/link-local/metadata IPs for user-supplied crawl URLs; revalidate DNS/redirect targets and restrict schemes. The separate local fixture/local-model adapters can use explicitly allowlisted loopback endpoints; the internet crawler cannot inherit that exception.
5. JS-only or inaccessible portfolios enter a visible manual-URL/import path. Do not quietly install a headless browser or scraping proxy. A bounded optional renderer can be added only after the core workflow works and its maintenance cost is documented.
6. Surface breadth limits: pages visited, links found, sources blocked, truncation and last refresh. Allow manual company URLs/CSV to make a reliable presentation independent of global search. No scraping of search-engine result pages as the unannounced default.

## OpenRouter replacement: extract locally

- Structured Gmail submissions: deterministic label parsing, MIME/HTML cleanup and contact normalization. Unstructured emails/website text: extract evidenced fields using JSON-LD, known labels and bounded heuristics. Keep source quotes, field confidence and unknowns; use a review queue when evidence is insufficient.
- Qualification is deterministic application code. A model never gets authority to change the mandate, initiate network requests, write a sheet, decide an investment or override provenance checks.
- Optional local inference: support an already-running, user-selected Ollama model on an explicit loopback endpoint, with schema-constrained JSON, short bounded prompts, cancellation and output validation. Check capabilities first; model names should come from the installed-model list rather than a fabricated default. Do not download multi-gigabyte models or install a system service just to pass a demo. When unavailable, deterministic extraction/review still works.
- Explicitly disallow Ollama cloud endpoints and cloud-backed automatic fallbacks in the local/no-paid mode. A local HTTP gateway alone does not establish that inference is local. Display provider/model/mode and what data is being sent; skip local inference unless the model is actually installed locally. Do not use a hosted 'free tier' as the zero-cost guarantee.
- Track counters: pages fetched/cache hits, parsed/reviewed messages, model requests if opted in, provider errors, and configured paid adapters. Default test fixtures must record zero requests to Tavily/OpenRouter or model cloud endpoints.

## Keep the dependencies honest

Required services: this application and its local SQLite file. Live Google integration uses the existing Gmail/Sheets accounts and quota; do not promise unlimited API usage or universal consent availability. Mandatory external Go modules are the chosen SQLite driver and standards-aware HTML parser, including their actual transitive closure. A local model server is optional. No SDK/agent framework/vector database/browser automation stack is needed for the baseline.

Primary references checked when preparing the handoff:

- Go HTTP server/client: https://pkg.go.dev/net/http
- Go templates/static embedding: https://pkg.go.dev/html/template and https://pkg.go.dev/embed
- SQLite driver: https://pkg.go.dev/modernc.org/sqlite
- HTML parser: https://pkg.go.dev/golang.org/x/net/html
- Gmail paginated message listing: https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/list
- Sheets append and returned updates: https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append
- Ollama distinguishes local and cloud endpoints: https://docs.ollama.com/api/introduction

Pin and verify actual current module/toolchain versions during implementation. The documents describe an implementation decision; they do not claim that the Go replacement already exists.
