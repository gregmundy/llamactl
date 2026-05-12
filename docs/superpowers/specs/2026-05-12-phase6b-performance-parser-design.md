# llamactl Phase 6b: Performance + parser hardening — Forward-looking Design Sketch

**Status:** Draft 2026-05-12 (forward-looking; not approved for implementation)
**Ships as:** `v1.4.0` (provisional)
**Branch:** `phase6b-performance-parser` (provisional)
**Covers:** hot model swap, speculative decoding auto-config, GGUF tensor-shape inference for parameter-count fallback.

---

## 1. Status of this document

This is a **forward-looking design sketch**, not an approved implementation spec. The Phase 6a brainstorming session (2026-05-12) selected Option A — write three sequential phase specs today, but only implement Phase 6a immediately. Phases 6b and 6c plans sit ready for later sessions.

When the user is ready to start Phase 6b, this document needs to graduate into an approved spec via a fresh brainstorming session that resolves the open design tensions listed in §4. Do **not** invoke `writing-plans` against this document as-is.

---

## 2. Theme

Three items that share a common surface: model loading and runtime behavior.

| Item | Phase 6a triage list # | Effort estimate |
|---|---|---|
| Hot model swap | 4 | L (5-7 days) |
| Speculative decoding auto-config | 7 | M (3-4 days) |
| GGUF tensor-shape inference for ParamsCount | 16 | M-L (2-4 days) |

All three are independent in scope but pair naturally: the GGUF parser hardening lets the speculative-decoding draft-model selection work reliably on community quants, and hot swap shares request-draining infrastructure with anything that touches in-flight requests.

Ships as `v1.4.0` — feature additions, no breaking API changes anticipated.

---

## 3. Scope sketch (subject to brainstorming refinement)

### 3.1 Hot model swap

User-facing: `llamactl serve <new_id>` when a different model is already running on the same port → drains in-flight requests, stops old, starts new, preserves the port. Or a dedicated `llamactl swap <new_id>` command. **Open: command shape.**

Architecture: requires either a llamactl-managed proxy in front of llama-server (so we can absorb requests during the swap window) OR a graceful-handoff protocol where the old llama-server keeps serving until the new one is healthy. The proxy approach is more invasive but gives us hooks for future features (metrics, auth interception, model routing). The handoff approach is simpler but leaves a few-second window of port-binding races.

### 3.2 Speculative decoding auto-config

User-facing: when serving model X, llamactl detects whether a smaller draft model from the same family is locally installed (e.g., `qwen2.5-3b-instruct` as draft for `qwen2.5-32b-instruct`). If present, automatically wires `--model-draft`, `--ctx-size-draft`, etc.

The auto-detection requires:
- Family match: derive from `Arch` + naming convention (`qwen2.5-*` family).
- Size ratio: draft should be 5-10× smaller than main model for speedup to outweigh draft overhead.
- KV-cache budget: draft adds its own KV cache; verdict needs to account for combined footprint.

Could add `llamactl fit` understanding draft-pair candidates in the ranked table (additional `--draft <id>` column or row pairing).

### 3.3 GGUF tensor-shape inference

User-facing: nothing new. `list` shows real param counts for files that lack both `general.parameter_count` AND `general.size_label` (deferred from Phase 6a; some older fine-tunes carry only `general.architecture`/`name`/`file_type`).

Implementation: extend `internal/gguf/header.go` to read past the kv-block into the tensor info block (currently the parser stops at kv-block). For each tensor, the GGUF format encodes a `name + n_dims + dims[]`. Find `token_embd.weight` — its second dimension is `hidden_dim`. Combined with `block_count` (already in kv-block as `<arch>.block_count`) and an arch-specific heuristic, derive paramsB to within ~10%.

Alternative: count all tensors, sum their byte sizes, divide by quant-specific bytes-per-param. Less accurate but doesn't depend on architecture-specific layout knowledge.

`readLimit` of 64 MiB may need to grow — tensor info section can be larger than the kv block for big models. Need to measure.

---

## 4. Open design tensions to resolve before plan-writing

| # | Tension | Options |
|---|---|---|
| 1 | Hot swap mechanism | (a) llamactl-managed HTTP proxy in front of llama-server, (b) graceful handoff with port-race window, (c) defer entirely if too complex |
| 2 | Hot swap command shape | (a) `llamactl serve <new>` auto-detects swap, (b) explicit `llamactl swap <new>`, (c) flag: `serve --replace` |
| 3 | Speculative decoding detection | (a) inspect installed models, match by family prefix, (b) require explicit `--draft <id>` flag, (c) detect at recipe level (e.g., `code-with-draft` recipe) |
| 4 | Speculative decoding eligibility | (a) hardcoded family map (qwen, llama, mistral), (b) GGUF architecture string + size ratio heuristic, (c) user-confirmed via `llamactl fit --speculative` |
| 5 | GGUF tensor-shape parsing scope | (a) just `token_embd.weight` + arch heuristic, (b) full tensor info parse for completeness, (c) defer until a real user complaint surfaces |
| 6 | Combined or split | All three in 6b, or split into 6b (hot swap) + 6c-prequel (spec decoding + parser)? |

---

## 5. Dependencies on Phase 6a

- `Deps.UserHomeDir` injection (Phase 6a #23) — useful for hot-swap testing.
- `ParamsB float64` migration (Phase 6a #20) — speculative decoding's draft-vs-main ratio math is cleaner with float64.
- Doctor's `authOnPublicBindCheck` + plist scanning helpers (Phase 6a §4.3) — hot swap reuses launchd plist enumeration to find the existing service to replace.
- `update` doctor check (Phase 6a §5.4) — same pattern reused for tracking llama-server binary updates.

None of these are hard blockers, but landing 6a first keeps the diff focused.

---

## 6. Provisional acceptance criteria

(Sketch — full criteria established at brainstorming time.)

- `llamactl serve <new>` (or `swap`) while another model serves on port P → new model serves on P after a window measured in seconds, with in-flight requests handled gracefully.
- Speculative decoding auto-fires when a viable draft is installed; `llamactl status` surfaces the active draft pairing.
- `cmd/gguf-inspect` against files lacking both `parameter_count` and `size_label` reports non-zero ParamsCount.

---

## 7. Out of scope for Phase 6b

- Multi-tenant serving (different api_keys per model — Phase 6a stayed shared-token).
- Web UI for hot swap operations (deferred to Phase 6c).
- Linux/Intel support (perma-deferred per Phase 6a triage).
- Local quantization pipeline (perma-deferred).

---

## 8. References

- Phase 6a spec (`2026-05-12-phase6a-cli-completions-design.md`) — items #4, #7, #16 in the triage table.
- PRD §Out of scope, post-v1 candidates (lines 398-408) for speculative decoding.
- PRD §Non-goals (line 59) for hot model swapping (re-elevated per Phase 6a triage).
- llama.cpp documentation for `--model-draft`, `--ctx-size-draft` flags.
- GGUF v3 spec for tensor info block layout.
