# "Was great, now struggling" — diagnosis (2026-06-11)

Investigation into why the agent hacks worse deep into a session / after recent changes.

## Two separate causes

### 1. Option A (my change) — REVERTED
Switching shell execution to isolated-by-default broke persistent shell state (cd / env /
reverse shells / foothold continuity) that the box's exploit chain — and the model's learned
workflow — relied on. **Reverted**: shell execution is back to the persistent shared terminal
by default, matching the build that hacked the box. Kept the *hardening* (no-pager /
non-interactive env, `sudo -n`) and all the HTML-entity encoding fixes — those are pure upside.

### 2. Context exhaustion + a compaction-memory pollution loop (environmental)
- The in-use profile **`qwen3.6-nothink` is capped at 32,001 tokens.** Compaction fires at 85%
  (~27k). A multi-hour box hack blows past that repeatedly; each compaction drops detail
  (which ports, creds, foothold), so the model gets "dumber" the deeper it goes.
- **Worse, a feedback loop was eating that tiny window:** `rememberCompactionSummary` saved a
  *new* "Compacted session state" memory at **importance 4** on every compaction (empty ID →
  `SaveMemoryEntry` appended instead of replacing). **10 of 19 memories** were these. The
  top-8 memory injection then spent ~2.4k tokens re-injecting stale compaction echoes (some
  literally *"No structured decisions were recovered by fallback compaction"*), crowding the
  context → causing more compaction. Self-reinforcing.

## Fixes applied (build + tests green)

- **Compaction memory no longer piles up.** `rememberCompactionSummary` now uses a stable
  per-workspace ID, so each compaction **overwrites** the single session-state memory (1, not
  10+), and importance dropped 4 → 2 so real facts/decisions win the injection slots.
- **Pruned the existing pile**: memory 19 → 10 (removed 9 stale compaction entries, kept the
  newest + all real facts/decisions). Backup at `~/.config/mauler/memory.json.bak-pre-prune`.

## Recommended (needs you — not code)

**Raise the context window.** 32k is small for box work on a 24GB 3090 running a 27B Q4. You
already run `qwen3.6-large-code` at **64k** on the same box, so the VRAM headroom exists.
- Relaunch InferenceBridge / llama.cpp for the nothink model at a larger context (try
  **49,152**, or 65,536 if it fits), then bump that profile's `ctx_tokens` to match. Don't
  bump the profile alone — it must match what the server actually loaded or requests overflow.
- This is the single biggest lever for "stays sharp through the whole box."

**Optional:** for long exploitation, consider the `qwen3.6-think` profile — reasoning on helps
multi-step privesc, at some speed cost. `nothink` is faster but plans worse.

## Net
Revert removed the regression I introduced; the memory-pollution fix + prune reclaim ~2k
tokens of context per run and stop the loop; raising the context window is the durable fix for
depth. Encoding fixes and terminal hardening from earlier remain in place.
