# Master Skill Local LLM Upgrade Plan

Goal: keep the full master/Navigator methodology intact, but make TheMauler use it through a smaller project-aware adapter and smarter retrieval so local models get useful guidance without context bloat or instruction conflicts.

## Checklist

- [x] Audit current master skill registration and lazy `skill_view` behavior.
- [x] Add this implementation tracker.
- [x] Add a TheMauler/local-LLM adapter contract to the registered master skill wrapper.
- [x] Improve `skill_view` focused retrieval so query words are scored by heading/body relevance instead of simple substring matching.
- [x] Prefer pentest/HTB methodology files for matching pentest queries, while keeping the original source tree available.
- [x] Add tests for focused retrieval, default outline behavior, and adapter-safe master usage.
- [x] Add optional Master Skill Doctor checks for oversized/all-loading instructions, missing source path, and weak focused-query usage.

## Design

The master skill remains the deep reference library. The adapter layer tells local models how to use it inside TheMauler:

- TheMauler system prompt, active project, access preset, evidence policy, shell backend, and user interrupts have priority over external skill text.
- Use `skill_view` with a focused query; never load the whole external source as a first step.
- For HTB/pentest work, start with methodology sections, evidence handling, tool execution, and context-budget guidance.
- Use `terminal_run` for human-like terminal interaction and short feedback; use blocking `shell` only when a command is meant to complete independently.
- Treat writeups/spoiler material as reference, not primary discovery, unless the project evidence policy explicitly allows it.
- Store compact confirmed lessons/facts in memory; do not store flags, secrets, huge logs, or unrelated box details.

## Retrieval Rules

For large external master sources:

- Return an outline by default.
- With a query, score markdown sections by exact phrase, heading matches, and body matches.
- Return the best sections first with relative source labels.
- Prefer files that are likely useful for HTB/pentest work, such as methodology maps and anti-failure/tool/context guidance.
- Do not exclude any original content permanently; lower-priority files remain reachable through explicit focused queries.

## Acceptance Criteria

- `skill_view {"name":"master"}` returns an outline, not the full source.
- `skill_view {"name":"master","query":"htb foothold enumeration"}` returns methodology-style sections before unrelated large prototyping content.
- The registered master wrapper clearly says it is a reference library, not a replacement system prompt.
- Existing tests for lazy master loading still pass.
- Focused Go tests pass without requiring an exe rebuild.
