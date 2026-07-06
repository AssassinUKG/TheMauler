# Agent Loops & Local-LLM Stability — Research Report

**Date:** 2026-06-22
**Scope:** Hard comparison of TheMauler's agent loop against the current state of the art for stability, tool-calling reliability, and "just working" on local models (RTX 3090 / Qwen3.6-class, llama.cpp + InferenceBridge).
**Method:** Read TheMauler's loop end-to-end (`internal/app/app.go` ~L2140–2865, `internal/llm/stream.go`, `internal/llm/client.go`), then researched online references and benchmarks.

---

## Implementation status (2026-06-22)

The gaps below were acted on in this session:

| Gap | Status | Where |
|---|---|---|
| §6.1 Generic duplicate read-only tool detector | **Done** | `internal/app/agent_loop_stability.go` (`repeatedIdenticalReadBlock`), registered in `preToolRecoveryRules`; escalation in `repeatedPreToolRecoveryIgnored` |
| §6.5 `tool_choice=required` on inspection/ops first turn | **Done** | `toolChoiceFor` + empty-defs guard in `toolDefsAndChoiceForTurn` |
| §6.6 Model tool-calling-tier guardrail | **Done** | `doctor.go` `addModelTierCheck` + `modelParamBillions` |
| §6.4 llama.cpp agent launch-flag advisory in doctor | **Done** | `doctor.go` `addLlamacppAgentFlagAdvisory` |
| §6.3 Auto lint/test after edits | **Already existed** | `mutation_verifier.go` `appendLint`→`lintFile` (`go vet`/`py_compile`/`bash -n` after every write/edit). Only a heavier *project test/build* step remains optional. |
| §6.2 Grammar-constrained tool args | **Held** | `llm.Request.JSONSchema` plumbing exists, but llama.cpp's `json_schema` constrains whole-message content, not the tool-call envelope; shipping it blind risks breaking all tool calling. Needs validation against a live llama.cpp server before enabling. |

All new logic is covered by `internal/app/agent_loop_stability_test.go`; full `go vet ./...` + `go test ./...` pass.

---

## 0. TL;DR verdict

You were told right: the "loop" (the harness around the model) is where stability comes from, not the model. NVIDIA's Jensen Huang made exactly this point at GTC Taipei 2026 — *"the model is the brain, the harness is the body, the tools work in a runtime."* The harness owns the lifecycle: understand intent → observe → reason → plan → call tools → juggle working vs. long-term memory → iterate.

**The honest finding: TheMauler is not behind the field — on loop-level stability engineering it is ahead of most open reference loops, and substantially ahead of naive framework loops (LangChain-style "just append and re-call").** Your loop already implements the majority of what the 12-Factor Agents canon, the "unreasonable effectiveness" thread, and the MLflow/Statsig production guides recommend — and it adds a layer of **local-model-specific recovery** (truncation chunking, inline-tool-markup repair, thinking-block suppression, `[DONE]`-without-finish_reason flushing) that none of the generic frameworks have, because they assume a hosted frontier model that doesn't fail those ways.

Where you have real gaps, they are narrow and fixable — and most are at the **llama.cpp launch-flag / backend layer**, not in your Go loop. Details in §6–7.

---

## 1. What "an agent loop" actually is (the reference model)

Every credible source converges on the same minimal structure:

```
context = [system, user]
loop:
    response = LLM(context, tools)          # model picks next step as structured output
    if response is terminal (plain text / "done" tool): break
    result = execute(response.tool_calls)   # deterministic harness code runs the tool
    context += [response, result]           # append and re-ask
```

- **Hacker News "unreasonable effectiveness of an LLM agent loop with tool use"** (#43998472): *"95% of the magic is in the LLM itself and how it's been fine-tuned to do tool calls."* The loop is trivial; the reliability work is everything wrapped around it.
- **Jensen Huang, GTC Taipei 2026:** an agent is *"a large language model (or many) sitting inside a harness, and that harness orchestrates it to do productive work … understands intent, observes context, reasons, plans, calls tools, and juggles working memory with long-term memory."* He framed each stage of the loop as activating different parts of the datacenter — i.e. the loop is now the *unit of computation*, not the single forward pass. NVIDIA even shipped **OpenClaw**, pitched as "the operating system of agentic computers," and **Nemotron 3 Ultra** "built for long-running agents." Your instinct to study loops is aligned with where the whole industry is pointing.

The key insight from the field: **the naive loop breaks at scale.** Per 12-Factor Agents, "anything more than 10–20 turns becomes a big mess the LLM can't recover from … agents get lost when the context window gets too long — they spin out trying the same broken approach over and over." Getting from 90% → 99% reliability is where all the engineering goes.

---

## 2. The reference "best practice" checklist (synthesised from sources)

| # | Principle | Source |
|---|-----------|--------|
| A | **Hard budgets** (steps, time, tokens, tool-call count) → halt + escalate, never run forever | MLflow, Statsig, 12-Factor |
| B | **Loop / duplicate-call detection** → inject "you already tried this" or terminate | MLflow, Barun Saha (small-model agents) |
| C | **Own your control flow** — explicit switch/for, not a framework's opaque loop; break for human/long tasks | 12-Factor #8 |
| D | **Own your context window** — active curation, summarization, hierarchical memory; stay out of the ">40% dumb zone" | 12-Factor #3 |
| E | **Compact errors into context** — don't dump raw stack traces repeatedly; restructure | 12-Factor #9 |
| F | **Tools are just structured outputs** — use native function-calling, strict typed schemas, narrow tools, lean outputs | 12-Factor #4, MLflow, Statsig |
| G | **Validate at the tool boundary** — reject/fix/escalate hallucinated args, no silent failures | MLflow, Statsig |
| H | **Truncate large tool outputs** so one web page can't eat a small model's whole window | Barun Saha, MLflow |
| I | **Automated feedback** — run lint/types/tests after edits, feed errors back | HN thread, Aider/OpenHands pattern |
| J | **Small, focused agents / sub-agents** — shorter context = fewer spin-outs | 12-Factor #10 |
| K | **Pause / resume / serialize** the loop state | 12-Factor #6, #12 |
| L | **Native tool-call parsing robustness** for local models (`<tool_call>` XML leaks, thinking-block capture) | llama.cpp issues #20837/#20809/#21264 |

---

## 3. TheMauler's loop — anatomy

The loop lives in `runAgent` (`internal/app/app.go`, `agentLoop:` at L2173). What it actually does each turn:

**Per-turn setup**
- Recomputes tool defs + `tool_choice` (`toolDefsAndChoiceForTurn`, L8586): conversational first turn → `none`; otherwise `auto`; mid-task always `auto`.
- Honors a **tool-call budget** (`cfg.Agents.MaxToolCalls`) — when exhausted, strips tools, sets `tool_choice=none`, and forces a final text-only summary (L2181–2193). ✅ Principle A.
- **Context-window management**: `NeedsCompactionWithReserve`, `ClearOldToolResults(N)`, and `doCompact` (summarization) before each call, plus a *post-response* check against backend-reported `prompt_tokens` that triggers compaction next turn (`backendUsagePressure`, L2229–2261, 2463). ✅ Principle D.
- **Memory re-injection**: re-scores stored memories against recent context and surfaces newly-relevant ones mid-run (`maybeReinjectMemory`, L2222) — hierarchical/long-term memory layer. ✅ Principle D/Huang's "long-term memory."
- **Persist-before-loss nudge**: first time context is dropped, injects a system message telling the model to write findings to a file/memory *now* (L2265–2271). This is a genuinely novel mitigation for the "dumb zone" data-loss problem.

**Local-model thinking control**
- Tool-enabled turns now force thinking off from the first tool turn, and `NoThinkAfterToolCalls` defaults to 2 as a fallback. Qwen3 tends to place tool calls inside the `<think>` block when thinking is on and context is heavy with prior tool results, which causes grammar-triggered early termination. This directly targets llama.cpp issue #20837. ✅ Principle L.

**Backend-failure resilience (pre-output)**
- Up to **15 retries with exponential backoff** (1→2→4→8→15s cap) for recoverable inference failures *before any output* — explicitly to ride out a managed llama-server restart / 27B reload (`maxPreOutputInferenceRetries=15`, L2304–2338, 7098–7126). ✅ This is beyond what generic frameworks do; it's tuned to your InferenceBridge reality.

**Output parsing & repair**
- `ParseSSE` (`stream.go`) handles three local-backend quirks generic clients miss: (1) `reasoning_content` *and* `reasoning` aliases; (2) `[DONE]` arriving with accumulated tool-call fragments but **no** `finish_reason="tool_calls"` (flushes instead of dropping the call); (3) **truncated tool-JSON detection** — if accumulated args aren't valid JSON, it reports `Truncated` so the recovery path fires instead of executing a broken call. Synthesizes stable `call_N` IDs for empty-ID local tool calls. ✅ Principle L, robustly.
- **Inline tool-markup repair** (`parseInlineToolMarkup`, L2391–2407): when a model emits `<tool_call>…`/XML as *text* instead of structured calls, it converts them back to real calls. Bounded by `maxMalformedToolContinues=2`. ✅ Principle L — this is exactly the llama.cpp #20809/opencode #24316 "naked tool call in console" failure, handled.

**No-progress / incompleteness recovery** (the heart of the stability work, L2473–2650)
- Distinguishes and handles separately: thinking-only empty responses, `finish_reason=length` truncation, "narrated an action but made no call" (`looksAboutToAct`), and "looks incomplete." Each gets a *targeted* continue prompt — notably the **chunked-write directive**: when output is too big for `max_tokens`, it instructs `write_file` first section then `append=true` for the rest (L2543–2552, 2576–2581). Bounded by `maxAutoContinues=8`. ✅ Principles B/E, and a local-model-specific answer to the 17.4%-truncated-thinking problem.

**Tool execution** (L2671–2862)
- Per-call: state event, budget `before`/`after` gates, **duplicate-fetch skip** (`duplicateFetchURLSkip`), **command-storm hint** (`shellCommandStormHint` — nudges scripting a repeated command family), confirmation gating for dangerous tools, rollback snapshot for writes, mutation verification, secret-redaction guardrail, empty-shell-output hinting, and tool-result truncation (`MaxToolResultChars`). ✅ Principles B, G, H, and more.

---

## 4. Feature-by-feature comparison

| Capability (from §2) | Reference SOTA | TheMauler | Verdict |
|---|---|---|---|
| A. Hard budgets (steps/tokens/tools) | Recommended; often missing in DIY loops | `maxAutoContinues=8`, `MaxToolCalls`, per-tool `taskBudget`, context budget | **Exceeds** — multi-dimensional |
| B. Loop/duplicate detection | "track consecutive identical calls, inject nudge, then terminate" | `duplicateFetchURLSkip`, `shellCommandStormHint`, `noToolContinues` streak, `malformedToolContinues` | **Meets+** (see gap §6.1) |
| C. Own control flow | Explicit switch/for, break for humans | Hand-rolled Go loop, confirmation gating, `awaitConfirm` | **Meets** |
| D. Own context window | Summarize, budget, hierarchical memory, <40% | Compaction + `ClearOldToolResults` + memory re-injection + persist-nudge | **Exceeds** |
| E. Compact errors | Restructure, don't re-dump | `toolErrorResult` + recovery hints, shell-result summarization | **Meets** |
| F. Native tool calls / strict schema | Use native FC, typed, narrow | Native FC via OpenAI-compat; **no strict JSON-schema grammar enforcement on tool args by default** | **Gap §6.2** |
| G. Boundary validation | reject/fix/escalate | `normalizeToolCallArguments`, malformed-arg detection + retry hint | **Meets** |
| H. Truncate tool outputs | Mandatory for small ctx | `MaxToolResultChars`, shell summarization | **Meets** |
| I. Automated feedback (lint/test) | "ridiculously effective" | `lint.go`, `mutation_verifier.go` exist; **not auto-run in the loop after every edit** | **Partial §6.3** |
| J. Small/sub-agents | #1 anti-spinout | `subagents.go` exists (`subagent_research`) | **Meets** (could lean harder, §7) |
| K. Pause/resume/serialize | Serialize context | History persisted; full pause/resume of a mid-run loop not first-class | **Partial** |
| L. Local tool-call parsing | jinja, reasoning-format, thinking off | SSE quirk handling + inline repair + force-no-think | **Exceeds** (in-loop); **backend flags = §6.4** |

---

## 5. Where TheMauler is genuinely ahead of the reference loops

1. **Local-failure taxonomy.** Generic frameworks (LangChain, naive ReAct) assume a hosted frontier model. They have *no* concept of `[DONE]`-without-finish_reason, truncated-tool-JSON, thinking-block tool capture, or inline `<tool_call>` text leakage. You handle all four. This is the single biggest reason DIY loops "feel unstable" on local models and yours doesn't.
2. **Truncation → chunked write.** The field's data shows 17.4% of reasoning outputs truncate with no answer; your loop turns that failure into a *recoverable* "write first section, append the rest" instruction. Most loops just error out.
3. **Persist-before-context-loss nudge.** A direct mitigation for the 12-Factor ">40% dumb zone." I haven't seen another open loop do this proactively.
4. **Backend-restart tolerance (15× backoff).** Tuned to a self-hosted managed llama.cpp bridge that frontier-API loops never face.
5. **Force-no-think after N tool calls.** A precise, evidence-backed fix for the #1 Qwen3-on-llama.cpp agent failure.

---

## 6. Real gaps (ranked by impact on your stability)

### 6.1 Generic loop-detection is incomplete — only `fetch_url` and shell-storms are caught
You detect duplicate **fetch_url** and shell **command storms**, but the MLflow/Barun-Saha guidance is broader: *any* tool called with *identical args* N times in a row should trip a nudge-then-terminate. A model stuck re-`read_file`-ing the same path, or re-running the same `grep`, currently only burns budget. **Recommendation:** add a generic `(tool_name + normalized_args)` repetition counter in the tool-exec loop; at 2 consecutive identical calls inject "you already ran this exact call; the result is unchanged — change approach or stop," at 3 force a recovery summary. This is ~30 lines and closes the most common small-model spin-out.

### 6.2 No grammar-constrained tool arguments by default
Your `Request` supports `JSONSchema` (grammar-constrained output) and llama.cpp/LM Studio support it, but the default agent turn sends tools as plain OpenAI function defs and relies on the model to emit valid JSON, then repairs after the fact. The field's strongest reliability lever for small models is **forcing** valid tool JSON at generation time (GBNF grammar) rather than repairing. **Recommendation:** when the backend is llama.cpp and `tool_choice` would be `required`/`auto` with a single likely tool, attach a JSON-schema grammar for that tool's args. Eliminates the malformed-args class entirely instead of catching it.

### 6.3 Lint/test feedback isn't auto-run in the loop
`lint.go` and `mutation_verifier.go` exist, and writes are verified, but the HN thread's highest-leverage trick — *automatically run lint/type-check/tests after each edit and feed errors straight back into context* — isn't wired as an automatic post-edit step. **Recommendation:** after a successful `write_file`/`edit_file` to a code file, optionally auto-run the project's lint/build and append failures as a tool result. Turns the model's blind edits into a grounded edit-verify loop (this is exactly how Aider/OpenHands get reliable).

### 6.4 Backend launch flags are the highest-ROI fix and live *outside* your Go loop
Per llama.cpp issues #20837/#20809/#21264 and the Qwen docs, the baseline that makes Qwen3.6 tool-calling stable is:
- `--jinja` (without it `<tool_call>` XML and `</think>` leak — this is *the* fix; your inline-repair is a safety net for when it's missing)
- `--reasoning-format deepseek` (Qwen3 uses `<think>`/`</think>` like DeepSeek-R1)
- **disable speculative decoding** when you see truncation/repetition (your `SpecType`/`SpecDraftNMax` knobs — specdec rejections at `</think>` spike EOS prob and cause exactly the early-termination you're defending against in code)
- Q5_K_XL / Q6_K_XL over Q4 for tool-calling accuracy (BFCL shows a sharp capability cliff below ~7–9B *effective* quality)
- `--presence-penalty` up to 2.0 if you see thinking loops
- `--reasoning-budget N` (llama.cpp PR #20297) to cap thinking tokens at generation time

**Recommendation:** have `doctor.go` assert these flags on the managed bridge and warn loudly if missing. Fixing the backend config removes the *root cause* of several failure modes your loop currently spends effort *recovering* from. (You already partly do backend mismatch detection via `recordBackendRuntimeMismatch` — extend it to flag-level checks.)

### 6.5 `tool_choice` heuristic is coarse
`toolChoiceFor` only ever returns `none`/`auto` and is essentially binary on the first turn. The OpenAI/12-Factor guidance is to also use `required` when the task clearly needs a tool first (your `needsInspectionTool`/`needsOperationalTool` recovery prompts prove you *know* when that's true — but only in the recovery path, not on turn one). **Recommendation:** when `needsInspectionTool(firstUserText)` is true on turn one, send `tool_choice=required` so the model can't burn the first turn narrating. Small win, removes one class of "talked instead of acting" continues.

### 6.6 Capability floor on the model itself
Field benchmark to internalize: Docker's 21-model / 3,570-test agent eval found **Qwen3 14B ≈ 0.971 vs llama3.3-70B ≈ 0.607** at tool calling — bigger ≠ better. BFCL V4 shows a cliff: Qwen3.5 ~9B ≈ 66%, but 4B ≈ 50%, 2B ≈ 44%. On a 3090/24GB, this says: **stay at the ~9B+ tool-tuned tier (or your 27B/MoE) and prefer a higher quant; don't drop to 4B to fit a bigger context.** No code change — a config/profile guardrail (your `runtimeprofile` registry could warn when a sub-7B model is selected for an agent task).

---

## 7. Recommended priority order

1. **§6.4 backend flags + doctor assertions** — highest ROI, fixes root causes, zero loop changes.
2. **§6.1 generic duplicate-call detector** — ~30 lines, kills the most common spin-out.
3. **§6.3 auto lint/test feedback after edits** — biggest jump in *coding* reliability.
4. **§6.2 grammar-constrained tool args** on llama.cpp — eliminates malformed-args class.
5. **§6.5 `tool_choice=required` on turn one for inspection/ops tasks** — small, easy.
6. **§6.6 model-tier guardrail** in profile selection — cheap insurance.

Items 1–2 are the ones that will most visibly improve "does it just work."

---

## 8. Bottom line

Your premise is correct and current: per Huang and the whole 12-Factor / "unreasonable effectiveness" canon, **the loop is the product** and stability is engineered into the harness, not bought with parameters. TheMauler's loop already implements the canon and then adds a local-model recovery layer the generic frameworks lack — so it is *ahead* of the typical reference loop for your exact use case. The remaining gains are concentrated, not architectural: tighten the backend launch flags (§6.4), generalise loop-detection (§6.1), and close the edit→verify feedback loop (§6.3).

---

## Sources

- [The unreasonable effectiveness of an LLM agent loop with tool use — Hacker News](https://news.ycombinator.com/item?id=43998472)
- [12-Factor Agents (HumanLayer) — GitHub](https://github.com/humanlayer/12-factor-agents) · [Factor 8: Own Your Control Flow](https://github.com/humanlayer/12-factor-agents/blob/main/content/factor-08-own-your-control-flow.md)
- [12 Factor Agents — HumanLayer Blog](https://www.humanlayer.dev/blog/12-factor-agents)
- [AI Agent Tool Use Best Practices for Practitioners — MLflow](https://mlflow.org/articles/ai-agent-tool-use-best-practices-for-practitioners/)
- [LLM APIs in Agent Loops: What Actually Breaks at Scale — DEV](https://dev.to/supertrained/llm-apis-in-agent-loops-what-actually-breaks-at-scale-454o)
- [Tool calling optimization — Statsig](https://www.statsig.com/perspectives/tool-calling-optimization)
- [What Happens When Your AI Agent Gets Stuck? (small-model agents) — Barun Saha](https://medium.com/@barunsaha/what-happens-when-your-ai-agent-gets-stuck-building-reliable-agents-for-small-language-models-a5e7a32cd03d)
- [The biggest local LLM is useless if it can't call a tool — XDA](https://www.xda-developers.com/biggest-local-llm-machine-useless-cant-call-single-tool-how-many-parameters/)
- [When Local LLM Agents Know but Don't Call Tools — Medium](https://medium.com/@michael.hannecke/your-local-agent-knows-when-it-needs-a-tool-whether-it-calls-one-is-a-different-question-a2dae65ef84e)
- [NVIDIA GTC Taipei/COMPUTEX 2026 — NVIDIA Blog](https://blogs.nvidia.com/blog/nvidia-gtc-taipei-computex-2026-news/) · [Five thoughts from Huang's GTC Taipei 2026 keynote — SiliconANGLE](https://siliconangle.com/2026/06/01/five-thoughts-nvidia-ceo-jensen-huangs-gtc-taipei-2026-keynote/) · [TradingKey keynote recap](https://www.tradingkey.com/analysis/stocks/us-stocks/261938407-nvda-verarubin-ai-jensenhuang-nvidia-tradingkey)
- [The Only Correct Way to Use llama.cpp with Qwen3.6-27B — GoPenAI](https://blog.gopenai.com/the-only-correct-way-to-use-llama-cpp-with-qwen3-6-27b-d550bd0605a7)
- [llama.cpp #20837 — Qwen3.5 tool calls inside thinking block](https://github.com/ggml-org/llama.cpp/issues/20837) · [#20809 — Qwen3-Instruct tool calling broken by false thinking detection](https://github.com/ggml-org/llama.cpp/issues/20809) · [#21264 — line-level repetition detection](https://github.com/ggml-org/llama.cpp/issues/21264)
- [Why Qwen3.5 Falls Into Infinite Thinking — Moonglade](https://lyn.one/reasoning-control-flow)
- [Troubleshooting llama.cpp Tool Calls — netclaw](https://netclaw.dev/troubleshooting/llama-cpp/)
- [Qwen — Run locally with llama.cpp](https://qwen.readthedocs.io/en/latest/run_locally/llama.cpp.html)
