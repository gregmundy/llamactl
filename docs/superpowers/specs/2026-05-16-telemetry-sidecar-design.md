# llamactl Telemetry Sidecar — Design Spec

**Status:** Approved 2026-05-16
**Ships as:** `v1.6.0`
**Branch:** `telemetry-sidecar`
**Covers:** a new sidecar binary `llamactl-telemetryd` that exposes installed-model and running-model telemetry over HTTP, consumed by an external website running on a different host. Plus the one-line recipe change to enable `--metrics` on every `llama-server` invocation.

---

## 1. Goal

The user wants their personal website (running on a different machine, reachable via Tailnet) to render a widget showing:

- Names and sizes of locally installed models
- Number of currently running models
- Per running model: name, on-disk size, current tokens/sec (or idle state), hours active

Today, all of this data is either on the local filesystem (`~/.config/llamactl/models/*.json`, launchd plists) or behind per-port `llama-server` HTTP endpoints (`/metrics`, `/slots`, `/props`). A remote consumer cannot reach the filesystem data, and the "installed-but-not-running" list has no HTTP source at all.

This spec adds a small read-only HTTP aggregator (`llamactl-telemetryd`) that runs on the same host as the model services, scrapes the local backends on a timer, and exposes one combined JSON endpoint over the network.

The core `llamactl` binary remains a one-shot CLI. A clarification to CLAUDE.md's "Don't add a daemon" rule decided 2026-05-16: the rule applies to `cmd/llamactl/` specifically, not to the repo. A second binary in `cmd/` is precedented (`gguf-inspect`).

Ships as `v1.6.0` — minor bump because a new binary surface is added, no breaking API changes.

Out of scope: web UI, hot model swap, metrics history / time-series, multi-user auth.

---

## 2. Architecture overview

New code lives in two places: a new `cmd/llamactl-telemetryd/` binary entry point and a new `internal/telemetry/` package containing the daemon's logic.

```
cmd/llamactl-telemetryd/main.go    NEW: wiring only — Deps construction + http.Server.ListenAndServe

internal/telemetry/                NEW package
├── deps.go                        Deps struct + narrow interfaces (HTTPClient, Clock, FS reads, LaunchdLister)
├── adapters.go                    Production adapters for the interfaces
├── poller.go                      Background tick goroutine — scrape + state update
├── backend.go                     Single-backend scrape (parses /metrics, /slots, /props)
├── aggregator.go                  Builds the /v1/telemetry response from cached state + installed list
├── server.go                      net/http.Handler + Bearer-token auth middleware
└── state.go                       In-memory: last Sample per modelID (for tok/s delta)

internal/cli/telemetry.go          NEW: `llamactl telemetry enable/disable/status` cobra commands
internal/launchd/telemetryd.go     NEW: render/bootstrap/bootout the com.llamactl.telemetryd plist
internal/launchd/ports.go          + PortsByLabel(dir) map[string]int helper (id → port)

internal/recipes/recipes.go        + append "--metrics" to every recipe's argv (one line)
internal/cli/doctor.go             + telemetryAuthCheck (14 → 15 checks)
internal/cli/deps.go               (unchanged — telemetry has its own Deps)
internal/config/config.go          + 3 new fields: TelemetryPort, TelemetryHost, TelemetryInterval
```

**Conventions preserved (per CLAUDE.md):**

- `cmd/llamactl-telemetryd/main.go` is wiring only — all daemon logic in `internal/telemetry/`.
- `internal/telemetry/Deps` follows the same narrow-interface pattern as `internal/cli/Deps`. Production wiring in `adapters.go`; tests construct `&Deps{...}` with fakes.
- No raw `exec.Command` outside `runner.CommandRunner` (the daemon doesn't shell out at all — pure HTTP + file reads).
- `ErrUserError` for user-facing errors from the management commands.
- Storage paths: `~/Library/LaunchAgents/com.llamactl.telemetryd.plist`, `~/Library/Logs/llamactl/telemetryd.log` (rotated at 10 MiB / 3 generations, parallel to per-model logs).

---

## 3. Management commands (inside `llamactl`)

All lifecycle commands live in the main `llamactl` binary. The sidecar binary itself takes no subcommands — when invoked it just runs the HTTP server until SIGTERM. Only launchd invokes the sidecar binary; the user manages it through `llamactl`.

```
$ llamactl telemetry enable
telemetryd started on http://0.0.0.0:18080 (pid=51234)

$ llamactl telemetry status
telemetryd: running (pid=51234, port=18080, host=0.0.0.0, uptime=2h13m)

$ llamactl telemetry disable
telemetryd stopped
```

| Subcommand | Behavior |
|---|---|
| `telemetry enable` | Validates config (api_key required when host==0.0.0.0), renders plist, `launchctl bootstrap`. Waits up to 5s for the port to bind; reports endpoint URL. |
| `telemetry disable` | `launchctl bootout` and removes the plist. Idempotent — exits cleanly if not running. |
| `telemetry status` | Prints running/stopped, pid, port, host, uptime. `--json` flag emits the same fields as JSON. |

**Pre-enable validation:**
- If `telemetry_host == "0.0.0.0"` AND `api_key` empty → `ErrUserError` exit 2 with message pointing at `config set api_key` or `config set telemetry_host 127.0.0.1`.
- If `telemetry_port` is already bound by something else → `ErrUserError` with message pointing at `config set telemetry_port`.

---

## 4. Config additions

Three new keys in `~/.config/llamactl/config.yaml`, exposed through the existing `config get/set/list` machinery (Phase 6a):

| Key | Type | Default | Purpose |
|---|---|---|---|
| `telemetry_port` | int (1-65535) | `18080` | Port `llamactl-telemetryd` binds |
| `telemetry_host` | string | `0.0.0.0` | Bind address; `127.0.0.1` for local-only |
| `telemetry_interval` | duration | `2s` | Poll interval; parsed via `time.ParseDuration` |

Validation lives in the existing `config.Set` reflection-based allowlist — adding fields to the struct auto-extends the allowlist.

The existing `api_key` field is reused (Phase 6a). Precedence: `LLAMACTL_API_KEY` env > config. Telemetryd reads it at startup.

---

## 5. The HTTP API

### 5.1 Endpoints

| Method | Path | Auth | Response |
|---|---|---|---|
| GET | `/v1/telemetry` | Bearer (when api_key set) | 200 `application/json` aggregate |
| GET | `/health` | none | 200 `{"status":"ok"}` (for launchd's port-bind wait and external healthchecks) |
| any | other | — | 404 / 405 |

### 5.2 Request: `GET /v1/telemetry`

```
GET /v1/telemetry HTTP/1.1
Host: llm-mini.tailnet.ts.net:18080
Authorization: Bearer sk-your-token-here
```

### 5.3 Response shape

```json
{
  "generated_at": "2026-05-16T14:23:11Z",
  "installed": [
    {
      "id": "qwen2.5-3b-instruct",
      "params_b": 3.0,
      "quant": "Q5_K_M",
      "size_bytes": 2469606195,
      "added_at": "2026-05-12T09:14:00Z",
      "last_served_at": "2026-05-13T18:02:00Z"
    },
    {
      "id": "qwen3-1.7b",
      "params_b": 1.7,
      "quant": "Q4_K_M",
      "size_bytes": 1288490188,
      "added_at": "2026-05-13T11:30:00Z",
      "last_served_at": null
    }
  ],
  "running": [
    {
      "id": "qwen2.5-3b-instruct",
      "port": 8082,
      "recipe": "chat",
      "size_bytes": 2469606195,
      "memory_bytes": 695894016,
      "state": "idle",
      "tokens_per_second": 0.0,
      "tokens_predicted_total": 1280,
      "uptime_seconds": 3621
    }
  ]
}
```

### 5.4 Field semantics

**`installed[].*`** — read from `~/.config/llamactl/models/<id>.json` plus an `os.Stat` on the gguf path. `last_served_at` is `null` when the model has never been served.

**`running[].state`** — enumerated:

| Value | Meaning |
|---|---|
| `idle` | Backend reachable, no slot processing |
| `active` | At least one slot has `is_processing: true` OR `requests_processing > 0` |
| `loading` | Backend returns 503 on /health (model still loading) |
| `metrics_disabled` | /metrics returns 501 (backend started without `--metrics`; pre-v1.6.0 plist still loaded) |
| `unreachable` | Scrape connect/timeout failure (port not bound, process crashed) |

**`running[].tokens_per_second`** — float, computed as `Δ(tokens_predicted_total) / Δ(tokens_predicted_seconds_total)` between the two most recent samples for that backend.

- `null` when fewer than two samples exist (first poll after daemon startup, or first poll after a backend appears) OR when `state` ∈ {`loading`, `metrics_disabled`, `unreachable`}.
- `0.0` when there have been polls but no generation has happened between them.
- Reflects the rolling rate over the most recent inter-poll interval (default ~2s).

**`running[].uptime_seconds`** — integer seconds since the launchd-managed process started. Sourced from `proc.Etime` (the same path used by `status`).

**`running[].memory_bytes`** — RSS at last sample, same source as `status --json`.

**`generated_at`** — RFC 3339 timestamp of the most recent poll cycle. Useful for the consumer to detect a stuck daemon.

### 5.5 Auth

When `api_key` is configured (env or config):

- Request without `Authorization` header → `401 {"error":"unauthorized"}`
- Header with wrong scheme or wrong token → `401 {"error":"unauthorized"}`
- Header `Authorization: Bearer <api_key>` → handler runs

When `api_key` is empty (only possible when `telemetry_host == 127.0.0.1`, since startup validation refuses 0.0.0.0 + empty):

- Auth middleware is a no-op; any request reaches the handler.

`/health` is always unauthenticated (parity with llama-server's `/health`).

---

## 6. Data flow

### 6.1 Steady-state poll loop

```
every telemetry_interval (default 2s):
  1. List ~/Library/LaunchAgents/com.llamactl.*.plist
     - For each non-telemetryd plist, parse {id, port, recipe} from <ProgramArguments>
  2. For each (id, port), fan out three parallel HTTP scrapes (1s timeout each):
       GET http://127.0.0.1:<port>/metrics   → Prometheus text
       GET http://127.0.0.1:<port>/slots     → JSON array
       GET http://127.0.0.1:<port>/props     → JSON
  3. For each backend, compute new Sample and store under id:
       Sample{
         scraped_at: time.Now()
         tokens_predicted_total: <counter>
         tokens_predicted_seconds_total: <counter>
         requests_processing: <gauge>
         slots_busy: any(is_processing)
         memory_bytes: from proc.RSS(pid) (separate cheap call)
         scrape_error: nil | error
       }
     Derive state, compute tokens_per_second from delta vs previous Sample.
  4. Read ~/.config/llamactl/models/*.json + os.Stat each gguf → installed[]
  5. Atomic swap a new aggregate {installed, running, generated_at} into the served cache.
```

### 6.2 Request handling

```
GET /v1/telemetry
  → auth middleware (Bearer match if configured)
  → handler: marshal cached aggregate as JSON, write 200
```

Constant-time per request. No backend scrapes happen on the request path.

### 6.3 Tok/s edge cases

| Situation | Result |
|---|---|
| First poll after daemon startup | `tokens_per_second: null` (no prior sample) |
| Second poll, no requests served between polls | `tokens_per_second: 0.0` |
| Two polls during active generation | `tokens_per_second: <rolling rate>` |
| Backend started without `--metrics` | `state: "metrics_disabled"`, `tokens_per_second: null`; `idle`/`active` derivable from `/slots` so state still has meaning |
| Backend mid-load (503 on /health) | `state: "loading"`, all numerics null |
| Backend port not yet bound | `state: "unreachable"`, all numerics null |

### 6.4 Failure isolation

Each backend scrape runs in its own goroutine with a 1s `context.WithTimeout`. One hung backend never blocks the others. Worst-case poll duration is bounded by the timeout, not the slowest backend.

---

## 7. Error handling

### 7.1 Daemon startup (fail-fast)

| Condition | Behavior |
|---|---|
| Port in use | Exit non-zero with message pointing at `config set telemetry_port` |
| `telemetry_host == "0.0.0.0"` AND `api_key` empty | Refuse to start; message points at `config set api_key` |
| Config unreadable | Exit non-zero with the path |
| `~/Library/LaunchAgents` missing | Create it (matches existing `serve --detach` behavior) |
| `~/Library/Logs/llamactl/` missing | Create it |

### 7.2 Per-request

| Condition | Response |
|---|---|
| Missing/wrong Bearer (when api_key configured) | `401 {"error":"unauthorized"}` |
| Path other than `/v1/telemetry` or `/health` | `404` |
| Method other than GET | `405` |
| Internal panic in handler | Recovered, logged at error level, `500` |

### 7.3 Poll loop (resilient, never fatal)

- Each backend scrape error → recorded on the per-backend Sample, surfaced in the response via `state` field. Logged at debug level (so it's quiet under normal operation but visible with `log_level: debug`).
- Plist dir transiently unreadable → log warning, return empty `running[]` on the next aggregate, keep polling.
- Models dir transiently unreadable → empty `installed[]`, same.
- The poll loop never exits on errors. Only SIGTERM stops it.

### 7.4 Graceful shutdown

SIGTERM (sent by `launchctl bootout` when `telemetry disable` runs) → `http.Server.Shutdown` with 5s drain → exit.

### 7.5 LaunchAgent KeepAlive policy

**`KeepAlive: false`** for the telemetryd plist — explicitly different from model-serve plists. Reasoning: if the daemon crashes, we'd rather see it stay down than thrash a restart loop. Telemetry is not load-bearing for any model-serving behavior; user can `telemetry enable` again to recover.

`RunAtLoad: true` (survives reboot) and `ProcessType: Background` (low priority).

### 7.6 Doctor checks (14 → 15)

Single combined check `telemetryAuthCheck`:

| State | Verdict |
|---|---|
| No telemetryd plist | pass (telemetry not enabled) |
| Plist exists, `telemetry_host == "127.0.0.1"` | pass (local-only, no auth needed) |
| Plist exists, `telemetry_host == "0.0.0.0"`, `api_key` set | pass |
| Plist exists, `telemetry_host == "0.0.0.0"`, `api_key` empty | **warning** ("telemetryd is exposed without authentication") |
| Plist exists, port not reachable via dial-probe | **warning** ("telemetryd is enabled but not responding") |

---

## 8. Recipe change (one line)

`internal/recipes/recipes.go`'s `FlagsFor` function appends `"--metrics"` to every recipe's argv. Reasoning:

1. Tok/s computation in the sidecar requires the `/metrics` endpoint enabled.
2. Cost of enabling is negligible (a handful of counters in llama-server).
3. Users who upgrade `llamactl` but don't restart existing services get graceful degradation — those backends show `state: "metrics_disabled"` instead of producing an error. The next time they `serve` or restart, `--metrics` is included automatically.

Existing `recipes_test.go` snapshot tests are updated to include `--metrics` in every expected argv.

---

## 9. Out of scope

- **Web UI** — design decision: this binary serves JSON only. Cancelled per Phase 6c rationale.
- **Hot model swap controls (start/stop via API)** — telemetry is read-only.
- **Metrics history / time-series** — caller can poll and store its own history if it wants charts.
- **Multi-tenant auth** — single shared api_key only.
- **CORS headers** — consumer is server-to-server (website backend ↔ telemetryd). If a future use case needs browser-direct access, add `Access-Control-Allow-Origin` then.
- **Rate limiting** — single-user Tailnet deployment.
- **Prometheus passthrough** — considered (`/metrics` aggregating all backends with `model_id` labels), declined for YAGNI. Add later if a use case appears.

---

## 10. Acceptance criteria

1. `brew upgrade llamactl` installs both `/opt/homebrew/bin/llamactl` and `/opt/homebrew/bin/llamactl-telemetryd`.
2. With no prior config beyond `api_key`, `llamactl telemetry enable` succeeds, binds 0.0.0.0:18080, and `llamactl telemetry status` reports running.
3. `curl -H "Authorization: Bearer $LLAMACTL_API_KEY" http://localhost:18080/v1/telemetry` returns a 200 JSON document matching §5.3 with `installed[]` listing every entry in `~/.config/llamactl/models/`.
4. After `llamactl serve <id> --detach`, that id appears in `running[]` within `telemetry_interval` seconds (default 2s) with `state == "idle"` and a populated `port`, `recipe`, `memory_bytes`, `uptime_seconds`.
5. Driving a completion via `/v1/chat/completions` and re-scraping within 5s yields a non-null `tokens_per_second` reflecting actual throughput (verifiable against `/metrics` directly).
6. `llamactl stop <id>` removes the entry from `running[]` within `telemetry_interval` seconds.
7. Without `Authorization` header (and `api_key` set), `/v1/telemetry` returns 401.
8. `llamactl doctor` passes 15/15 with telemetryd running.
9. Refusing to start when `telemetry_host == "0.0.0.0"` AND `api_key` empty.
10. README has a new "Telemetry API" section documenting `/v1/telemetry` shape + curl example.

---

## 11. Dependencies

- Phase 6a's `Deps.Config` plumbing for the new config keys.
- Phase 6a's `api_key` field and env-var precedence.
- Phase 6a's `config get/set/list` reflection-based allowlist (extends automatically when fields are added).
- Phase 5's launchd plist parsing utilities (`launchd.PortsInUse`, `launchd.PortFor`, `launchd.HasAPIKey`) — one new helper added (`PortsByLabel`).
- v1.4.6's `proc.portInUse` dial-probe pattern, reused for the doctor reachability check.

---

## 12. Testing

### 12.1 Unit

| File | Coverage |
|---|---|
| `internal/telemetry/backend_test.go` | Prometheus text parser: canned good output, malformed lines, missing series, HTTP 501/503 responses, /slots parsing |
| `internal/telemetry/poller_test.go` | Scripted HTTP client; state cache updates across ticks; tok/s delta math; first-poll null; second-poll-no-activity 0.0 |
| `internal/telemetry/aggregator_test.go` | Cache + installed list → expected JSON shape and field set |
| `internal/telemetry/server_test.go` | Auth middleware (missing/wrong/correct), method/path negative paths, handler returns marshaled cache |
| `internal/launchd/telemetryd_test.go` | Plist render matches golden; bootstrap/bootout via `FakeRunner` |
| `internal/cli/telemetry_test.go` | enable/disable/status command logic with fake `LaunchdService` |
| `internal/recipes/recipes_test.go` | Existing snapshot test updated — every recipe's args include `--metrics` |
| `internal/cli/doctor_test.go` | `telemetryAuthCheck` matrix from §7.6 |

### 12.2 Integration

`internal/telemetry/integration_test.go`:

- `httptest.Server` mounted as a fake llama-server with scripted `/metrics`, `/slots`, `/props` handlers.
- Real `telemetry.Server` started against it.
- Drive 3 poll ticks, hit `/v1/telemetry`, assert shape + values.
- Flip the fake to 501 mid-test; verify `state == "metrics_disabled"` and `tokens_per_second == null`.

### 12.3 Live smoke (pre-tag)

1. `go install ./cmd/llamactl ./cmd/llamactl-telemetryd`
2. `llamactl serve qwen2.5-0.5b-instruct --detach` — verify plist now embeds `--metrics`
3. `llamactl telemetry enable`
4. `curl -H "Authorization: Bearer $LLAMACTL_API_KEY" http://localhost:18080/v1/telemetry` — verify field set
5. Run a completion via `/v1/chat/completions`, scrape telemetry twice — verify `tokens_per_second` reflects throughput
6. `llamactl stop`, scrape — verify `running[]` empty within 2s
7. `llamactl telemetry disable` — verify port unbinds
8. `llamactl doctor` — 15/15

### 12.4 CI

Existing `go test ./... -race` covers it. Gofmt must be clean on new files (Go 1.26 strictness applies).

---

## 13. Release flow

Same as prior releases (CLAUDE.md §Release flow):

1. Merge `telemetry-sidecar` to `main`.
2. Tag `v1.6.0`.
3. GoReleaser builds darwin/arm64 with both binaries; cask publishes to homebrew-tap.
4. `brew upgrade llamactl` installs both binaries.
5. Update `project_state.md` memory with what shipped.
6. Update `docs/MANUAL.md` version table.

GoReleaser config needs one addition: a second `builds:` entry for `cmd/llamactl-telemetryd`. Cask must reference both binaries.

---

## 14. References

- PRD `docs/llamactl-prd-v1.5.md` — non-goals list excluded "web UI / dashboard"; telemetry sidecar is *not* a UI, so doesn't violate that line.
- CLAUDE.md §"Don't do these things" — "Don't add a daemon" rule clarified 2026-05-16: applies to `cmd/llamactl/` only. CLAUDE.md should be updated to reflect this when this spec ships.
- Phase 6a spec `2026-05-12-phase6a-cli-completions-design.md` — established the `api_key` config + env precedence + doctor warning pattern this work reuses.
- Phase 6c spec `2026-05-12-phase6c-web-ui-design.md` — cancelled; the rationale for cancelling (web UI inside `llamactl` re-shapes its identity) is preserved by keeping this work in a separate binary.
