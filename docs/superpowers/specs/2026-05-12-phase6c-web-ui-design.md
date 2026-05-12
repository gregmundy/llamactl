# llamactl Phase 6c: Web UI — Forward-looking Design Sketch

**Status:** Draft 2026-05-12 (forward-looking; not approved for implementation)
**Ships as:** `v1.5.0` (provisional)
**Branch:** `phase6c-web-ui` (provisional)
**Covers:** browser-based dashboard for status, serve/stop, model add/remove, and (optionally) fit search.

---

## 1. Status of this document

This is a **forward-looking design sketch**, not an approved implementation spec. Per the Phase 6a brainstorming session (2026-05-12), Phases 6b and 6c plans sit ready for later sessions.

A web UI is a significant addition — it introduces an entire new tech stack (HTTP server beyond the llama-server proxy, frontend framework, build tooling, distribution). Before this graduates to an approved spec, the user must brainstorm and resolve the architectural choices in §4.

Do **not** invoke `writing-plans` against this document as-is.

---

## 2. Theme

A single addition: a browser-accessible dashboard for the operations currently in the CLI.

| Item | Phase 6a triage list # | Effort estimate |
|---|---|---|
| Web UI / dashboard | 6 | L (1-2 weeks) |

Single big-ticket item. No backlog drain bundled — Phase 6a + 6b should have cleared the small stuff by the time 6c starts.

Ships as `v1.5.0`.

---

## 3. Scope sketch (subject to brainstorming refinement)

User-facing surface:
- Status page: running services (port, model, recipe, memory, uptime, tok/s, endpoint URL) — mirrors `llamactl status`.
- Controls: start/stop a service per row.
- Model management: list installed models with size/params/last-served; remove/purge.
- Fit search: form to query HF + ranked table (mirrors `llamactl fit`); install button per row.
- Doctor: 14-check status with remediation hints.
- Live: tail logs for a running service.
- Auth: shared with endpoint auth from Phase 6a (`api_key`), or session-cookie based with a separate token.

Out-of-scope candidates (decisions deferred to brainstorming):
- Streaming chat playground (would need to proxy chat-completions; nice-to-have).
- Metrics graphs (CPU/GPU/memory over time; requires recording).
- Multi-user (assume single-user-on-Tailnet model).

---

## 4. Open design tensions to resolve before plan-writing

| # | Tension | Options |
|---|---|---|
| 1 | Binary distribution | (a) single `llamactl` binary embeds frontend assets via `embed.FS`, (b) separate `llamactl-web` binary distributed via the same cask, (c) frontend bundled as a separate cask |
| 2 | HTTP server location | (a) llamactl process runs its own HTTP server on a configurable port (default :8081), (b) extend the existing llama-server proxy idea from 6b, (c) require manual `llamactl ui` start command |
| 3 | Frontend tech | (a) server-rendered HTML + HTMX (small bundle, no Node build), (b) Svelte/Preact + minimal API (modern feel, build tooling), (c) terminal-rendered web-tty (just expose existing CLI output via xterm.js) |
| 4 | Auth model | (a) reuse Phase 6a shared `api_key` (one token covers endpoint + UI), (b) separate UI token (`config set ui_token`), (c) session cookie + local-only bind (no token) |
| 5 | Network binding | (a) bind 0.0.0.0 by default (Tailnet access), (b) bind 127.0.0.1 (require SSH tunnel from another host), (c) configurable via `config set ui_host` |
| 6 | Lifecycle | (a) UI runs as a launchd LaunchAgent (same pattern as model services), (b) UI runs as part of the main llamactl process (always-on once `llamactl ui enable`), (c) ad-hoc `llamactl ui` foreground command |
| 7 | CORS / API surface | (a) UI calls internal Go functions directly (same binary), (b) UI calls a JSON API exposed by llamactl (cleaner separation, more code), (c) UI generates static HTML on each request (simplest, less interactive) |

---

## 5. Dependencies on Phase 6a + 6b

- Phase 6a's `Deps.Config` plumbing — UI reads same config keys (`default_port`, `api_key`, etc.).
- Phase 6a's `update` infrastructure — UI surfaces "newer version available" banner.
- Phase 6a's auth — if option (a) is chosen for tension #4, the UI shares the endpoint token.
- Phase 6b's hot swap (if landed) — UI exposes a "swap model" button.
- Phase 6b's speculative decoding — UI shows draft-pair status when active.

If 6b doesn't land before 6c, the UI scope shrinks (no hot-swap UI, no speculative-decoding indicator) but is still implementable.

---

## 6. Provisional architecture sketch

Two production-ready directions, exploration TBD:

### Direction A — HTMX + Go server-side rendering

- New package: `internal/web/` with `Server{}.Run(ctx, listener)`.
- Routes: `/` (status), `/models` (list), `/fit`, `/doctor`, `/logs/<id>` (SSE log tail).
- HTML templates via `text/template`; assets embedded with `embed.FS`.
- No Node toolchain. Frontend bundles total ~100 KB.
- Interactivity via HTMX's `hx-get`/`hx-post`/`hx-swap` for partial page updates.

Pros: smallest diff, no build step, ships in the same binary.
Cons: limited frontend richness (no real-time graphs, no rich animations).

### Direction B — Svelte/Preact + JSON API

- New `cmd/llamactl-web/` binary, OR `llamactl ui` subcommand.
- JSON API endpoints: `GET /api/status`, `POST /api/serve`, `DELETE /api/services/<id>`, etc.
- Frontend in a separate `web/` directory; Vite or esbuild builds to static assets; embedded via `embed.FS`.
- Requires Node+npm in the dev environment but final binary is still single-file.

Pros: modern UX, easy to add charts and rich interactions.
Cons: bigger dev dependency (Node), separate frontend build step in CI.

---

## 7. Provisional acceptance criteria

(Sketch — full criteria established at brainstorming time.)

- `llamactl ui enable` starts a persistent UI service; `llamactl ui disable` stops it.
- Browsing to `http://<tailnet-host>:8081` shows a styled status page reflecting `llamactl status`.
- Buttons to start/stop a service work end-to-end.
- Fit search form accepts query, returns ranked table with install action.
- Doctor page mirrors `llamactl doctor`'s 14 checks with remediation hints.
- Auth: if `api_key` is set (Phase 6a), the UI prompts for it on first load and stores in localStorage.
- README has a "Web UI" section with screenshots.

---

## 8. Out of scope for Phase 6c

- Multi-user auth + permissions.
- Chat playground (could be Phase 6d if there's demand; for now users hit the endpoint directly per Phase 5's README §Using the endpoint).
- Metrics history / time-series graphs (would need recording infrastructure).
- Mobile-optimized layouts (Tailscale Tailnet model assumes desktop access).

---

## 9. References

- Phase 6a spec (`2026-05-12-phase6a-cli-completions-design.md`) — establishes shared `Deps`, `Config`, auth, version-check infrastructure that 6c builds on.
- Phase 6b spec (`2026-05-12-phase6b-performance-parser-design.md`) — UI surfaces 6b features if they land first.
- PRD §Out of scope, post-v1 candidates (lines 398-408): "Web UI for management" was a stated post-v1 candidate.
- PRD §Non-goals (line 53): "Web UI or dashboard" — re-elevated for Phase 6c.
