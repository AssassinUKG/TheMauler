# Browser workflow assistant plan

Updated: 2026-09-14

## Objective

Make Project Agent honestly route and execute authorised browser workflows such as account creation,
forms, authenticated navigation, email verification, CAPTCHA, and MFA without pretending that a
fetch-only lane performed interactive work. Prefer Mauler's native `chromedp` browser. Keep Python
browser automation optional until it proves a capability or reliability advantage.

## Fixed boundaries

- A browser session belongs to one Mauler conversation. Cookies and page state cannot be borrowed by
  a different conversation.
- Login details and typed values are never echoed in tool results, status objects, logs, or React
  state.
- CAPTCHA, MFA, email verification, payments, consent, and unclear final submissions require a
  visible human handoff. Mauler must re-observe the page after resume.
- An ambiguous or cancelled submission is never automatically repeated. Observe current state first.
- Local fixtures are the acceptance path. A real site may be opened and observed only when the user
  authorises it; no real account, verification, or submission is created by the test suite.

## Slice 1 — implemented

### Honest routing and capability truth

- Account creation, signup, registration, form completion, login, email verification, CAPTCHA, MFA,
  one-time-code, and OTP wording route to the compact native `browser` tool.
- Browser-only wording no longer accidentally selects workspace `write`/`edit` because it contains
  the verb “create”. Explicit requests for files or artifacts still retain write/edit tools.
- Every turn continues to receive the exact routed tool list. When Browser is disabled, the prompt
  gives one actionable Inspector path and forbids pretending the workflow has started.

### Owned persistent native session

- Chrome/Edge is allocated once against a controller-generated conversation owner. Per-action timeout
  or cancellation no longer destroys the persistent browser target.
- Open, snapshot, click, type, extract, screenshot, status, pause, takeover, resume, and close share
  the same cookie/page state.
- A different owner is refused while a session is active. Clear Chat, load conversation, workspace
  switch, and app shutdown retire the owner and close the browser.
- Visible mode is explicit. A headless session cannot silently become a human-takeover window.

### Workbench controls

Services now contains a **Native browser workflow** control card with:

- readiness and enabled-tool truth;
- current title, URL, state, visibility, last action, and actionable error;
- visible browser launch by URL;
- **Pause**, **Take over**, **I’ve completed this—continue**, and **Stop** controls;
- explicit conversation-only cookie lifetime guidance.

Resume performs a controller-owned snapshot before automated interaction continues.

## Verified acceptance evidence

The local browser fixture covers:

- multi-page signup, native required-field validation, cookie persistence, and redirects;
- simulated email-verification handoff and post-resume dashboard observation;
- simulated manual pause where automated actions are blocked;
- conversation isolation with no cookie crossover;
- an in-flight cancelled submission that reaches the server exactly once;
- typed credentials not appearing in returned tool text.

The user-authorised live smoke opened and snapshotted `https://admin.clara.co/` successfully. It did
not enter credentials, submit the form, create an account, or perform verification.

Verification on 2026-09-14:

- `go test ./... -count=1` — pass
- `go vet ./...` — pass
- `go test -race ./internal/app ./internal/tools -count=1` — pass
- `npm run --prefix frontend build` — pass
- `wails build` — pass; canonical output `build/bin/TheMauler.exe`

## Slice 2 — implemented

### Deterministic takeover in Chat

- Model-issued pause/takeover now changes the descriptive run state to `waiting_user`, emits a
  run-owned handoff event, and blocks that exact tool call until the user resumes, stops the browser,
  or cancels the run.
- Chat shows a first-class takeover card with page title/URL, human-step guidance,
  **I’ve completed this—continue**, and **Stop browser**. Resume wakes the same run and performs the
  controller-owned fresh snapshot before further automation.

### Structured page controls and file evidence

- Every snapshot returns bounded structured metadata for visible interactive controls. Controller-
  issued refs such as `e1` are owner-scoped and replaced after navigation/DOM-changing actions;
  stale refs fail with an instruction to snapshot again. CSS remains an explicit fallback.
- Upload selects one regular file inside the authoritative workspace, follows real paths to block
  symlink escape, caps size at 256 MiB, never returns file content, and records relative path, size,
  and SHA-256 evidence.
- Download writes only under `.mauler/browser-downloads/<conversation-owner>`, waits for a stable
  completed file, records relative path/size/SHA-256, enters RunLedger through the tool result, and
  refreshes Explorer without clearing Chat.

### Classified recovery and reliability evidence

- Browser errors are classified as scope policy, stale element, ownership conflict, expired session,
  timeout, waiting user, user stopped, or unknown. Recovery declares retryability, observation-first,
  and ambiguity explicitly. Click, submitted type, upload, and download are never auto-retried after
  an uncertain outcome.
- A deterministic browser workflow pass^5 now starts five fresh conversation-owned Chrome/Edge
  sessions. Every pass snapshots stable refs, selects a scoped upload, downloads a fixture artifact,
  and verifies SHA-256 evidence. Result on 2026-09-14: **5/5**.

Verification on 2026-09-14:

- focused takeover/ref/upload/download/recovery tests — pass
- deterministic browser workflow reliability gate — 5/5
- `go test ./... -count=1` — pass
- `go vet ./...` — pass
- `go test -race ./internal/app ./internal/tools -count=1` — pass
- `npm run --prefix frontend build` — pass

## Slice 3 — implemented

### Multi-tab continuity and safe checkpoints

- A conversation-owned browser can open, list, switch, and close secondary tabs through stable
  controller-issued refs (`t1`, `t2`, and so on). Raw CDP target IDs are never model-facing, element
  refs are invalidated on tab changes, and the primary tab remains owned by the workflow lifecycle.
- Services shows the current tab/count and provides named checkpoint save/resume. Checkpoints retain
  only a sanitized HTTP(S) URL, title, visibility preference, version, and timestamp under the active
  workspace. Userinfo, query strings, fragments, cookies, credentials, and form values are not
  persisted. Resuming therefore reopens a clean browser session rather than restoring authentication.
- Cancellation fixtures cover open, snapshot, click, type, extract, upload, and download. Each phase
  returns promptly with a code-owned recovery class instead of hanging or silently repeating an
  ambiguous action.

### Workbench usability pass

- The title bar now identifies the active workbench page and shows compact live run or idle
  profile/autonomy context.
- Primary sidebar navigation uses readable icon-and-label buttons, stronger active-page/session
  states, honest saved/archive labels, and responsive title-bar behavior while retaining every
  existing resizer, collapsed rail, terminal boundary, and daily action.

Verification on 2026-09-14:

- focused multi-tab/checkpoint/all-phase-cancellation tests — pass
- deterministic browser workflow reliability gate — 5/5 through the complete tools suite
- `go test ./... -count=1` — pass
- `go vet ./...` — pass
- `go test -race ./internal/app ./internal/tools -count=1` — pass
- `npm run --prefix frontend build` — pass
- `wails build` — pass; canonical output `build/bin/TheMauler.exe`

## Implemented slice 4: first-class Chat browser controls

- Chat now has a persistent **Browser** button in its primary header, with a compact status dot and
  state label, so browser work no longer requires navigating to Services.
- The in-chat browser surface accepts a URL and starts an explicitly visible Chrome/Edge session.
  It reports readiness, controller state, current URL/title, tab count, and the active owner.
- The same surface provides **Take over**, **I've completed this—continue**, **Stop**, and manual
  refresh controls. Status refreshes every five seconds while the panel is mounted.
- A model-issued browser handoff opens this surface automatically. Resume first observes the page
  through the controller before the blocked run continues, preserving the existing ownership and
  ambiguous-action recovery rules.
- The browser still runs in its own native window rather than inside WebView2. Cookies and session
  state remain conversation-owned, and typed form values are never copied into Chat or React state.
- The run router now consumes the same conversation-owned browser status as the UI. An active
  session is represented in the fresh execution-state packet with sanitized URL/title/tab state;
  query strings, fragments, credentials, cookies, and form values are excluded.
- Deictic page requests such as “can you see it?” and “what is open?” are narrowed to a required
  `browser` snapshot. Shell and HTTP probes remain available only for explicit network/protocol
  checks, preventing a rendered-page question from falling into WSL curl.

Verification on 2026-09-14:

- `npm run --prefix frontend build` — pass
- focused active-browser routing and prompt-redaction tests — pass
- focused active-browser race gate — pass
- deterministic context-quality gate — 7/7 fixtures, 105/105 attempts
- `go test ./... -count=1` — pass
- `go vet ./...` — pass
- `go test -race ./internal/app ./internal/tools -count=1` — pass
- scoped handwritten diff check — pass
- `wails build` — pass; canonical output `build/bin/TheMauler.exe`

## Next slice

1. Connect browser artifacts to the stronger immutable evidence promotion/review surface rather than
   relying only on RunLedger tool-result hashes.
2. Run a supervised end-to-end local-model browser scenario, then repeat it as pass^k. This is
   separate from the deterministic native-browser 5/5 and must not be conflated with Agent Eval x5.
3. Add optional compact tab/checkpoint management to the new Chat browser surface after operator
   testing confirms which controls are useful outside Services.
4. Benchmark the optional Python browser agent only after the native path's expanded tests are stable.
   Keep it opt-in unless it materially improves success without weakening ownership or evidence rules.

## Operator repair — visible intent and blocked-host repetition

- A model-issued compact `browser` open no longer controls visibility by omission. When the user's
  original task explicitly says to open, show, or launch a browser, the controller rewrites that
  call to `visible=true`; an explicit headless/background request still wins.
- Chat identifies an active headless session and offers **Restart visible**, an explicit user-owned
  stop/reopen action that avoids pretending a hidden session can be taken over.
- Reopening a host that already returned CAPTCHA, Cloudflare, unusual-traffic, or access-denied
  evidence is skipped before navigation. The prior evidence is returned to the model with an
  instruction to change source/tool or summarize the blocker.
