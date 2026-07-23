# Telegram Bot Integration Plan - 2026-07-02

**Goal.** Add a Telegram bot to TheMauler that acts as a separate remote chat/control surface. It
must be able to control projects fully, drive the agent/terminal/tools with the same unrestricted
capability profile as the desktop app, and support voice messages so the user can talk to it.

**Reference implementation.** HelixClaw already has the right shape:

- `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-channels\src\telegram.rs`
  - Long-polling Bot API adapter using direct HTTP requests.
  - `getUpdates`, `sendMessage`, `editMessageText`, `deleteMessage`, `getFile`, `sendVoice`,
    `sendAudio`.
  - Markdown-ish to Telegram HTML conversion and 3800-character chunking.
  - Voice/audio download and transcription pipeline.
- `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-config\src\types.rs`
  - `TelegramConfig`, `TelegramAccountConfig`, `TtsConfig`.
- `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-transcribe\`
  - Embedded Whisper transcription path.
- `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-tts\`
  - Piper/Edge TTS, plus OGG/Opus voice-note conversion.

HelixClaw is Rust, so TheMauler should port the architecture and protocol behavior into Go rather
than embedding the Rust crates. Keep orchestration, settings, ledgering, and tool control in Go.

---

## Architecture

```
Telegram user
  -> Bot API long polling
  -> internal/telegram adapter
  -> channel session router
  -> Mauler agent loop / project controller / terminal tools
  -> streamed status + final reply
  -> optional TTS voice note
```

The Telegram bot is not another project tab. It is a channel session:

- Separate chat history from the desktop Chat tab.
- Can target the current project by default.
- Can switch/list/open projects explicitly.
- Can start/stop/inspect runs.
- Can read Run/Brain/log/facts state.
- Can drive terminal/session tools through the existing tool registry and state machine.
- Replies with compact status updates and final summaries without dumping huge tool output.

**P0 dependency implemented first pass:** channel messages must go through a Message Router / Channel Bus before
any Telegram transport talks to the agent. This prevents a Telegram side question from polluting or
stopping an active desktop/project run.

Implemented first pass:

- `internal/channelbus/`
  - `Envelope`, `Attachment`, `Route`, `Response`, `WorkItem`.
  - Lanes: `side_chat`, `control`, `work_request`, `interrupt`, `note`, `unknown`.
  - Router rules:
    - Plain messages are model-backed side chat, isolated from the desktop project transcript.
    - `/status`, `/facts`, `/terminal_read`, `/plan`, `/logs`, `/help` are control/read commands.
    - `/cmd` is the explicit project work request; `/run` and `/ops` are compatibility aliases for `/cmd`.
    - Simple deterministic terminal chores can route through `quick_action` instead of starting a full project run.
    - Work requests queue when a project run is already active.
    - Voice/audio attachments are marked on the route for later transcription handling.
  - In-memory work queue.
- SQLite persistence:
  - Schema v8 adds `channel_work_queue`.
  - `channelbus.NewPersistentQueue(db)` persists work items across app/queue instances.
  - `PopNext` marks queued work as `dispatching`; `Mark` persists final status.
- `internal/app/channel_bus.go`
  - `DispatchChannelMessage`.
  - `ListChannelWorkQueue`.
  - `GetChannelBusStatus`.
  - Side-chat model replies that do not write into the desktop Chat transcript.
  - Busy-run queueing for `/cmd`, quick actions, and side-chat questions.
  - Queue drain after the active run becomes idle, with queued replies sent back through Telegram.
  - Quick-action routing for simple terminal chores.
  - Control handlers for `/status`, `/facts`, `/terminal_read`, `/stop`, `/help`, `/plan`, `/logs`.
  - RunLedger events: `channel_message_in`, `channel_message_out`, `channel_error`,
    `channel_queue_dispatch`, `channel_sidechat_model_start`, `channel_sidechat_model_done`,
    `channel_sidechat_model_error`, and `channel_sidechat_model_empty`.

Implemented transport/runtime first pass:

- `internal/app/telegram_runtime.go`
  - Starts/stops long-polling when Settings `telegram.enabled=true`.
  - Uses `getMe`, `deleteWebhook`, `getUpdates`, `sendMessage`, `deleteMessage`, `getFile`, and
    file download through the Go Telegram client.
  - Persists update offsets per bot token to avoid replay storms.
  - Filters by allow-list and group mention rules.
  - De-duplicates repeated Telegram messages within the channel session.
  - Converts Telegram messages into `DispatchChannelMessage` envelopes and sends replies/chunks.
  - Downloads voice/audio files to local Mauler state and routes them as attachments; real STT is
    still a follow-up.
  - Records `telegram_start`, `telegram_stop`, `telegram_message_in`, `telegram_send`,
    `telegram_rejected`, `telegram_voice_in`, and `telegram_error` style ledger events.
- `frontend/src/components/TelegramPage.tsx`
  - Adds a first-class Telegram tab/page with chat-style conversation view, raw event detail,
    queue/status panels, manual send box, copy/export, and delete-message action.

---

## Configuration

Add settings fields under `settings.Settings`:

```go
type TelegramConfig struct {
    Enabled           bool     `toml:"enabled" json:"enabled"`
    Token             string   `toml:"token" json:"token"`
    BotUsername       string   `toml:"bot_username" json:"bot_username"`
    RequireMention    bool     `toml:"require_mention" json:"require_mention"`
    AllowFrom         []string `toml:"allow_from" json:"allow_from"`
    DefaultProject    string   `toml:"default_project" json:"default_project"`
    DefaultProfile    string   `toml:"default_profile" json:"default_profile"`
    DefaultMode       string   `toml:"default_mode" json:"default_mode"`       // auto/builder/fixer/reviewer/researcher/planner
    DefaultToolset    string   `toml:"default_toolset" json:"default_toolset"` // unrestricted by default
    SendProgress      bool     `toml:"send_progress" json:"send_progress"`
    ProgressIntervalS int      `toml:"progress_interval_s" json:"progress_interval_s"`
    VoiceReplies      string   `toml:"voice_replies" json:"voice_replies"` // off/on_voice/always
    TranscriptionURL  string   `toml:"transcription_url" json:"transcription_url"`
    TranscriptionMode string   `toml:"transcription_mode" json:"transcription_mode"` // mauler_audio/helix_style/openai_compat
}
```

Recommended default behavior:

- `enabled=false`.
- `require_mention=true` for groups, ignored for direct messages.
- `default_toolset="unrestricted"`.
- `send_progress=true`.
- `progress_interval_s=10`.
- `voice_replies="on_voice"`.

Token handling:

- Do not hardcode HelixClaw's key into the repo.
- Support `MAULER_TELEGRAM_TOKEN`.
- Support loading from TheMauler settings.
- If the user wants to reuse the HelixClaw token, add an import action that reads the local
  HelixClaw config and copies it into TheMauler settings.

---

## Backend Files

Add a new Go package:

- `internal/channelbus/` - **implemented first pass**
  - Channel-neutral message envelope, route classifier, route lanes, and work queue.
  - This is the mandatory isolation layer between Telegram/Discord/etc. and project runs.
  - Queue persistence is implemented via SQLite `channel_work_queue`.

- `internal/telegram/client.go`
  - Bot API client.
  - Methods: `GetMe`, `GetUpdates`, `SendMessage`, `EditMessage`, `DeleteMessage`, `GetFile`,
    `DownloadFile`, `SendVoice`, `SendAudio`, `AnswerCallbackQuery`.
  - **Started:** Go-native Bot API client implemented with direct `net/http`: `GetMe`,
    `GetUpdates`, `SendMessage`, `EditMessage`, `DeleteMessage`, `GetFile`, `DownloadFile`,
    `SendVoice`, and `SendAudio`. `AnswerCallbackQuery` remains open for inline buttons.
- `internal/telegram/types.go`
  - Telegram update/message/file/callback structs.
  - **Started:** user/chat/message/voice/audio/update/file/send-message structs added.
- `internal/telegram/format.go`
  - Telegram HTML escaping, Markdown-ish conversion, chunking around 3800 chars.
  - Port the HelixClaw behavior, including code fences and links.
  - **Started:** safe HTML escaping, bold/code/pre formatting, and 3800-char chunking added.
- `internal/telegram/bot.go`
  - Poll loop, update offsets, cancellation, lifecycle.
  - Converts Telegram updates into `ChannelMessage`.
  - **Implemented in app runtime:** the first pass lives in `internal/app/telegram_runtime.go` so it
    can access app state, settings, ledger, channel bus, and the queued-reply sender directly.
- `internal/telegram/session.go`
  - Session keying:
    - Direct: `telegram:direct:<user_id>`
    - Group: `telegram:group:<chat_id>`
    - Group thread if available: `telegram:group:<chat_id>:thread:<thread_id>`
- `internal/telegram/voice.go`
  - Voice download, OGG/Opus handling, transcription handoff, TTS reply handoff.
- `internal/telegram/commands.go`
  - Slash commands and concise command parser.

Wire into the app:

- `internal/app/app.go`
  - Start/stop Telegram bot in `OnStartup` / shutdown if enabled.
  - Wails bindings:
    - `StartTelegramBot()`
    - `StopTelegramBot()`
    - `GetTelegramStatus()`
    - `SendTelegramTestMessage(chatID string)`
    - `SendTelegramMessage(chatID string, text string)`
    - `DeleteTelegramMessage(chatID string, messageID string)`
- `internal/settings/model.go`, `defaults.go`, `load.go`, `save.go`
  - Add config.
- `frontend/src/components/SettingsModal.tsx`
  - **Done:** Settings tab: `Telegram`, including enable, token, username, allow-list, default
    project/profile/mode/toolset, progress interval, and voice settings.
- `frontend/src/components/TelegramPage.tsx`
  - **Done:** full Telegram page for bot status, queue, event log, chat view, send, and delete.
- `frontend/src/components/AgentPanel.tsx` / Workbench Inspector
  - Compact channel-bus/Telegram status is visible through Settings and Telegram page; keep
    extending the bottom toolbox popup rather than adding another large side panel.

---

## Agent Control Surface

Telegram needs a small command layer so it can control projects without bloating normal chat.

Required commands:

```text
/status
/projects
/project <name-or-path>
/cmd <prompt>
/run <prompt>        # compatibility alias for /cmd
/ops <prompt>        # compatibility alias for /cmd
/stop
/terminal
/terminal_read
/terminal_send <text-or-control>
/facts
/plan
/logs
/brain
/files
/file <path>
/artifact <result_id-or-path>
/voice on|off|always|on_voice
/help
```

Normal non-command messages should become a Telegram-channel agent turn. They should not pollute the
desktop Chat tab. Store them as channel sessions in the existing session store or a sibling channel
store.

Current routing rule: normal non-command messages become Telegram side-chat model turns by default.
They stay separate from the desktop project chat and do not start tools or stop an active project
run unless promoted by an explicit command. If a project run is busy, side-chat questions are queued
and answered after the run is idle.

Control behavior:

- `/cmd` starts a normal run in the selected project/profile/toolset.
- `/run` and `/ops` are accepted as compatibility aliases for `/cmd`; they do not force a separate specialised agent mode.
- Obvious quick terminal requests, such as opening a tmux session, may execute through the `quick_action` lane and existing terminal state machine without starting a full model run.
- `/stop` cancels the active run.
- `/terminal_read` reads current terminal state using the existing structured terminal screen.
- `/terminal_send` sends input through the existing terminal state machine, not direct writes.
- `/facts` returns the live Run Facts packet.
- `/artifact` uses `read_tool_result` or file read logic, chunked for Telegram.

Unrestricted mode:

- Use the existing `unrestricted` toolset/preset.
- Do not add new permission prompts for Telegram when `DefaultToolset=unrestricted`.
- Still route all execution through the existing tool registry/state machine so ledgering,
  contracts, tool-result offload, terminal safety-state, and verifier rules still work.

---

## Voice Capability

Incoming Telegram voice message flow:

1. Receive `message.voice` or `message.audio`.
2. Call `getFile`.
3. Download bytes from Telegram file endpoint.
4. Transcribe.
5. Treat the transcript as a normal Telegram-channel user message.
6. Reply with text and optionally voice.

Transcription options:

- **M1 fast port:** use the same voice sidecar planned in
  `docs/voice-audio-implementation-plan-2026-07.md`, or an OpenAI-compatible local transcription
  endpoint if already running.
- **M2 proper local Go path:** integrate TheMauler's future `internal/audio` STT engine once landed.
- **Compatibility fallback:** mimic HelixClaw's embedded Whisper chain concept, but implemented as
  Go-side interface calls rather than Rust crates.

Outgoing voice replies:

1. Clean final response for speech.
2. Synthesize via TheMauler audio engine.
3. Convert to OGG/Opus for Telegram `sendVoice` where possible.
4. If conversion is unavailable, send as `sendAudio`.

Settings:

- `voice_replies=off`: text only.
- `voice_replies=on_voice`: voice reply only when user sent voice.
- `voice_replies=always`: voice reply for every final answer.

Do not narrate raw tool spam. Voice replies should be concise final/progress summaries.

---

## Progress UX

Telegram does not dump every tool result. The native runtime now sends:

- Immediate acknowledgement with task, local model profile, and access preset.
- Edited progress message every `progress_interval_s`:
  - clear working/finished/failed/blocked/stopped heading
  - original task
  - human-readable stage
  - current tool/action without raw model counters
  - elapsed time
- Final message:
  - the exact final assistant answer shown in the Mauler UI
  - a guarded/redacted result from the latest result-bearing tool when the model only says a generic
    "completed successfully"
  - failure/block/stop reason and total duration

Use `editMessageText` for progress updates to avoid chat spam, with a fallback to new messages if
edit fails. Final-answer delivery is independent of the periodic-progress toggle: disabling progress
still sends the result. Immediate and queued `/cmd` runs register their Telegram destination before
the agent starts, preventing fast completions from being lost between queue dispatch and tracking.

---

## Ledger And Memory

Add ledger events:

- `telegram_start`
- `telegram_stop`
- `telegram_message_in`
- `telegram_message_out`
- `telegram_voice_in`
- `telegram_voice_transcribed`
- `telegram_progress_update`
- `telegram_error`

Do not store bot tokens in ledger/details. Redact file URLs and Telegram token-bearing API URLs.

Session memory:

- Telegram chat history is channel-scoped.
- Project facts remain project-scoped.
- A Telegram command can attach to a project, but Telegram chat itself remains separate from the
  desktop chat transcript.

---

## Milestones

### T1 - Text Bot MVP [done] first pass

- **Done:** channel bus / route isolation exists before transport wiring.
- **Done:** channel work queue is durable in SQLite with regression coverage.
- **Done:** Go-native Telegram Bot API client and Telegram HTML/chunk formatting exist with
  fake-server regression coverage.
- **Done:** Settings config and Telegram settings UI.
- **Done:** Long-polling runtime, direct-message support, allow-list, mention filtering, offset
  persistence, duplicate-message suppression, text replies with Telegram HTML formatting/chunking.
- **Done:** `/status`, `/cmd`, `/run`, `/stop`, `/facts`, `/help`, `/plan`, `/logs`, `/terminal_read`.
- **Done:** Separate side-chat prompt/session path that avoids desktop Chat pollution.
- **Done:** Telegram page for chat/event inspection, manual sends, and delete-message action.
- **Done:** Tests for Bot API payload formatting/chunking, runtime routing, allow-list, mentions,
  offsets, duplicate suppression, voice/audio download routing, queue persistence, and busy drain.

### T2 - Full Project Control [implemented; live smoke pending]

- `/projects`, `/project`, `/logs`, `/brain`, `/files`, `/file`, `/artifact`.
- Project switching without disturbing desktop UI unexpectedly.
- **Done:** `/projects`, `/project`, `/logs`, `/plan`, `/brain`, `/files`, `/file`, and `/artifact`
  are implemented. Workspace reads are containment-checked and bounded for Telegram output.
- **Done:** project switching applies the saved lab context and workspace, then resets chat/plan.
- **Done:** RunLedger events exist for Telegram/channel messages and progress updates edit one
  persistent Telegram message, with a normal-send fallback when editing fails.
- **Done:** `/cmd` completion forwards the real Mauler UI answer to Telegram. Generic completion
  prose falls back to the latest guarded result-bearing tool output, queued runs are tracked before
  start, and the final answer is still delivered when periodic progress updates are disabled.
- Tests for session keys and command parser.

### T3 - Terminal Control [implemented; live smoke pending]

- **Done:** `/terminal_read` routes through the existing structured terminal screen/state.
- **Done:** `/terminal_send` executes through the shared terminal state machine when idle and queues
  when agent/eval work is active, so it cannot type over another run.
- **Done:** Active terminal/session/ready/busy state is available through channel status and
  terminal-read output.
- **Done:** Terminal state machine prevents typing over listener/busy states in the shared tool path.

### T4 - Voice In [implemented; live smoke pending]

- **Done:** Voice/audio file lookup, download, local save, and channel attachment routing.
- **Done:** downloaded voice/audio is transcribed through the shared Mauler audio runtime.
- **Done:** the transcript becomes the isolated Telegram user turn and the reply includes a bounded
  transcript preview.

### T5 - Voice Out [implemented; live smoke pending]

- **Done:** shared TTS reply generation, OGG/Opus conversion, `sendVoice` with `sendAudio` fallback,
  and the `voice_replies` policy are wired.

### T6 - Settings And UI Polish [done] first pass

- **Done:** Settings tab.
- **Done:** Telegram page with status cards, queue, raw events, chat view, manual send, delete.
- **Done:** Last errors/rejections are visible through ledger-backed Telegram events.
- **Open:** Test-message button and HelixClaw import action.
- Allow importing Telegram token/settings from HelixClaw local config.

---

## Verification

Unit tests:

- Telegram HTML escaping/chunking.
- Command parser.
- Session key generation.
- Channel route isolation: side chat stays read-only; busy project work queues instead of
  interrupting.
- Persistent channel queue survives a new queue instance and supports pop/mark transitions.
- Busy-time Telegram messages drain after the active agent run finishes: queued work starts the next
  project run, quick actions execute when idle, and side-chat questions are answered after the run.
- Bot API request construction and fake-server handling for `getUpdates`, `sendMessage`, `getFile`,
  and file download.
- Telegram HTML escaping/chunking for bold/code/pre text.
- Voice mode decision: off/on_voice/always.
- Redaction of tokens in errors/ledger.

Integration tests with fake Telegram server:

- `getUpdates` polling and offset handling.
- `sendMessage`/`editMessageText` calls.
- `getFile` + file download.
- `sendVoice`/`sendAudio`.

Live smoke:

- DM `/status`.
- DM `/cmd say hi and then list current project facts`.
- DM `/stop` during a long run.
- DM `/terminal_read`.
- Send a Telegram voice note and confirm transcript/run/reply.

Build checks:

```powershell
go test ./internal/telegram ./internal/app ./internal/settings
go build ./...
cd frontend
npm run build
```

---

## Open Decisions

1. Should Telegram start with TheMauler launch, or only when toggled on in Settings?
   - Recommended: only when `telegram.enabled=true`.
2. Should the bot control the current desktop project by default or a configured default project?
   - Recommended: configured `default_project`, falling back to current Agent Root.
3. Should voice out be enabled before desktop voice is fully landed?
   - Recommended: voice-in first, voice-out after the shared audio engine exists.
4. Should group chats be enabled immediately?
   - Recommended: direct messages first, groups after mention/allowlist behavior is tested.
