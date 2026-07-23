# Model Lab timing and Fast Chat guide

## Why InferenceBridge and Mauler can show different results

An InferenceBridge Chat number and a Mauler Project Agent run are different workloads unless all of
these match:

- model id and quant;
- loaded context;
- sampling values and thinking mode;
- system/conversation prompt;
- tool schemas and tool choice;
- warm versus newly switched model state; and
- decode-only versus complete-request timing.

InferenceBridge can show internal decode throughput even when its OpenAI-compatible SSE response
contains token usage but no llama.cpp `timings` object. In that case Mauler must not invent a decode
figure. It reports warm end-to-end throughput and labels the source.

## Repaired Matrix sequence

Each model/context row now runs:

1. Force-load the exact model and requested context; record load request time.
2. Read back the actual context and warn on a mismatch.
3. Run one short warm-up; retain its result but exclude it from scoring and headline speed.
4. Run the primary text scenario three times.
5. Average text timing only across those three measured runs.
6. Run the required tool-call scenario separately.
7. Run the mini agent loop separately.

The Matrix columns mean:

- `Decode`: authoritative backend-reported generated-token throughput, when available.
- `Warm E2E`: completion tokens divided by complete measured request time.
- `Source`: `decode`, `E2E`, `est. E2E`, `mixed`, or `unknown`.
- `TTFT`: average first-payload time across measured text runs.
- `Load`: exact model/context load request time.
- `Total`: measured scenarios combined; never used as text throughput.
- `Tool` and `Loop`: protocol/reliability results, not speed inputs.

Historical runs remain readable. Missing fields display as unavailable rather than being silently
reinterpreted.

## Using a tested model in Chat

`Model Lab > Model Matrix > Use` is an explicit state-changing action. It:

1. creates or updates a stable `lab-<model>-<context>k` local profile using the tested runtime
   recommendation;
2. makes that profile the persistent local Chat default; and
3. refreshes profile selectors.

Merely selecting or benchmarking a Matrix model does not change Chat. This prevents a visible
InferenceBridge model and Mauler's active Chat profile from silently diverging.

## Fast Chat versus Project Agent

Fast Chat is the low-latency, no-tools lane. It includes only:

- a compact no-tools system instruction;
- the active local profile name;
- at most four recent user/assistant turn pairs; and
- the new user question.

It excludes workspace documents, project memory, tool schemas, control-plane state, planning,
verification, and reviewer passes. It cannot inspect or change the workspace.

Project Agent retains the complete reliability path for real work: task-aware project context,
tools, evidence, planning, recovery, and verification. The speed repair does not weaken that path.

## Fair comparison checklist

Before comparing models:

- use the same requested and actual context;
- use the same prompt and output cap;
- check the timing source;
- compare decode with decode or warm E2E with warm E2E;
- keep the model warm for interactive-speed comparisons;
- compare tool and loop reliability separately; and
- run pass^5 only after a clean single suite.

