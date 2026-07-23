# OpenRouter in TheMauler and HelixClaw

Both apps support OpenRouter from their graphical settings. Local inference is the permanent
default. OpenRouter is an explicit **one-task cloud boost**: it applies to the next task only
and automatically returns the conversation to its local model afterward.

## TheMauler

1. Open **Settings > Providers**.
2. Select **openrouter**.
3. Paste the key into **API key** and choose **Save API key**.
4. Choose **List models**. Selecting a model creates or updates a profile bound to the
   OpenRouter provider.
5. Open **Profiles**, review the context and generation settings, and save.
6. In **Chat**, use **Next task > Cloud once** beside the composer. The run ledger records
   the actual cloud profile, but the saved active profile remains local.

OpenRouter profiles cannot become the persistent active profile. They remain available for
the one-task selector; local profiles remain the only default-profile choices.

When a catalogue model reports its limits, Mauler now fills sensible cloud defaults for the
profile automatically:

- **128K working context** for models whose provider route supports at least that much.
- The provider/model maximum when it is lower than 128K.
- **32K** only as a conservative fallback when the catalogue does not report a limit.
- **8K maximum output**, lowered automatically when the provider or context window requires it.

The profile's **Context tokens** value is Mauler's working budget for prompt assembly and
compaction. It does not allocate cloud memory and it does not override the provider's hard
context limit. The model list shows both the provider maximum and the standard working budget.

The key is stored outside `profiles.toml` in the per-user `provider-secrets.json` file and
is not returned to the web UI. The UI exposes presence flags only. If
`OPENROUTER_API_KEY` exists, it takes precedence over the saved key. **Clear API key**
removes only the UI-managed value.

## HelixClaw

1. Open **Settings > API Keys**.
2. Paste the key into the masked **OpenRouter** field and choose **Save Keys**.
3. Open **Settings > Models**, reload **Available**, and select an OpenRouter model for the
   online stack.
4. In **Chat**, leave **Task model** on **Local** normally. Choose **Cloud once** immediately
   before the task that needs frontier reasoning.

HelixClaw restarts the model actor for that task without discarding the conversation
transcript. When the task finishes, it tears down the cloud actor; the next message respawns
the same conversation on the configured local model. The one-task choice is not persisted.

For **Cloud once**, HelixClaw uses a provider/model-aware working context automatically: up to
128K for a known large-context model, the lower model maximum when necessary, and 32K when the
model limit is unknown. The agent's **KV Cache Window** and **Prompt Context Limit** remain local
model controls and are not raised for the cloud task.

HelixClaw stores the submitted value in its existing provider configuration while keeping
the input blank after reload; the browser receives only environment/saved-key presence.
`OPENROUTER_API_KEY` takes precedence over the saved value.

OpenRouter's OpenAI-compatible API base is `https://openrouter.ai/api/v1`. The apps use
bearer authentication for chat and model-list requests. Never paste an API key into chat,
logs, profile names, or model fields.
