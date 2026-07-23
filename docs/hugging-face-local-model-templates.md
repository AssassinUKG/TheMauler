# Hugging Face local-model templates

Updated: 2026-07-23

## Outcome

Selecting a local or Hugging Face model in **Control Center > Providers** now asks Mauler's native Go
runtime registry for a model-family template before creating/updating the profile. A known model gets
family sampling, context, thinking, tool-protocol, adapter, and chat-template metadata immediately.
An unknown model keeps conservative generic settings and must be verified with **Benchmark LLM**.

This does not change the persistent local default, download models, or send keys/model data to a new
service.

## Why Mauler does not paste a generic chat template

Chat formatting is model-specific. Hugging Face stores a Jinja chat template with the tokenizer, and
tool templates may have separate/default behaviour. llama.cpp can consume the template embedded in a
GGUF and expose OpenAI-style tool calls when Jinja processing is enabled. Mauler therefore records
`embedded-gguf-jinja` and checks that InferenceBridge/llama.cpp Jinja support is enabled; it does not
silently force ChatML over an unfamiliar fine-tune.

References:

- [Hugging Face: writing a chat template](https://huggingface.co/docs/transformers/main/chat_templating_writing)
- [Hugging Face: tools and documents in chat templates](https://huggingface.co/docs/transformers/v4.49.0/chat_template_tools_and_documents)
- [llama.cpp function calling](https://github.com/ggml-org/llama.cpp/blob/master/docs/function-calling.md)

## Built-in templates

| Template | Match | Default context | No-thinking sampling | Tool path |
|---|---|---:|---|---|
| `qwen3.6-27b-unsloth-ud-q4-k-xl` | exact Unsloth `Qwen3.6-27B-UD-Q4_K_XL` GGUF | 35,000 | temp 0.2, top-p 0.95, top-k 20, min-p 0, repeat 1.05 | native OpenAI; grammar probe still required |
| `qwen3.6-27b-huihui-abliterated-mtp` | exact Huihui abliterated MTP GGUF names | 35,000 | temp 0.2, top-p 0.95, top-k 20, min-p 0, repeat 1.05 | native OpenAI |
| `qwen3.6-27b-hauhaucs-aggressive` | exact HauhauCS Aggressive GGUF names | 40,000 | temp 0.2, top-p 0.95, top-k 20, min-p 0, repeat 1.05 | native OpenAI |
| `qwen3.6-35b-a3b-heretic-native-mtp` | exact 35B-A3B heretic/native-MTP names | 16,384 | temp 0.2, top-p 0.95, top-k 20, min-p 0, repeat 1.05 | repair-text; live required-tool gate failed |
| `qwen3.6-27b` | other Qwen 3.6 27B names, including Fable/Fus | 32,768 | temp 0.2, top-p 0.95, top-k 20, min-p 0, repeat 1.05 | native OpenAI |
| `qwen3.5-qwythos-9b-1m` | exact Qwythos 9B Claude/Mythos 1M names | 65,536 | temp 0.6, top-p 0.95, top-k 20, repeat 1.05 | native OpenAI; non-MTP file stays MTP-off |
| `qwen3.5-4b` | Qwen3.5 4B ids | 65,536 | no-think temp 0.7, top-p 0.8, top-k 20, presence 1.5, repeat 1.0 | native OpenAI |
| `gemma4-12b` | Gemma 4 12B instruction/GGUF ids | 65,536 | temp 1.0, top-p 0.95, top-k 64, min-p 0, repeat 1.0 | native Gemma 4 function calling |
| `gemma4-e4b` | Gemma 4 E4B instruction/GGUF ids | 65,536 | temp 1.0, top-p 0.95, top-k 64, min-p 0, repeat 1.0 | native Gemma 4 function calling |
| `gemma4-26b-a4b-qat-hauhaucs-balanced` | exact HauhauCS Balanced QAT HF/GGUF ids | 40,000 | temp 0.6, top-p 0.9, top-k 64, min-p 0.05, repeat 1.1 | mixed/native + repair safety net |
| `gemma4-26b-a4b-qat` | generic Gemma 4 26B-A4B QAT | 49,152 | temp 1.0, top-p 0.95, top-k 64, min-p 0.05, repeat 1.0 | mixed/native + repair safety net |
| `gemma4-31b` | Gemma 4 31B ids | 32,768 | temp 0.7, top-p 0.9, top-k 40, min-p 0, repeat 1.05 | text repair fallback |

Matching now treats the selected model id as authoritative before considering the profile name. This
matters in Model Matrix, where one profile intentionally supplies sampling defaults while the model
id changes per row. Before the 2026-07-23 repair, a borrowed `qwen3.6-*` profile name could make
Gemma 4 12B/E4B and Qwen3.5 rows inherit the Qwen3.6 runtime template.

## Current small-model sources

Google's current Gemma 4 cards specify `temperature=1.0`, `top_p=0.95`, and `top_k=64`, support the
Jinja `enable_thinking` switch, and document native Gemma 4 function calling. Mauler preserves an
explicit thinking-profile choice, but keeps no-thinking as the default for tool-heavy local runs.
It does not paste a replacement template over the GGUF.

Qwen's Qwen3.5 4B card recommends `temperature=0.7`, `top_p=0.8`, `top_k=20`, and presence penalty
`1.5` for general non-thinking work. Qwen3.5 uses the template `enable_thinking` parameter rather
than Qwen3's `/think` and `/nothink` soft switch.

Qwythos is an exact Qwen3.5 9B fine-tune template. Its current GGUF card recommends
`temperature=0.6`, `top_p=0.95`, `top_k=20`, and repeat penalty `1.05`, advertises native function
calling, and distinguishes normal and MTP-bearing files. The installed
`Qwythos-9B-Claude-Mythos-5-1M-Q5_K_M.gguf` has no MTP marker, so Mauler leaves speculation off.
The model's one-million-token architecture claim is not used as a 24 GB VRAM default; 65,536 is the
starting local working budget and still requires a live context test.

Sources:

- [Google Gemma 4 12B model card](https://huggingface.co/google/gemma-4-12B-it)
- [Google Gemma 4 E4B generation configuration](https://huggingface.co/google/gemma-4-E4B-it/blob/main/generation_config.json)
- [Google Gemma 4 function-calling guide](https://ai.google.dev/gemma/docs/capabilities/text/function-calling-gemma4)
- [Qwen Qwen3.5 4B model card](https://huggingface.co/Qwen/Qwen3.5-4B)
- [Qwythos 9B GGUF model card](https://huggingface.co/empero-ai/Qwythos-9B-Claude-Mythos-5-1M-GGUF)

## Default local model

The built-in `qwen3.6-nothink` profile now defaults to the tournament-winning
`Qwen3.6-27B-UD-Q4_K_XL.gguf` through the `inference-bridge` provider at 35,000 context tokens.
This applies to fresh/default configurations. Existing named user profiles are not silently
rewritten; the current installation explicitly uses its verified winner profile as the persistent
local default.

MTP remains artifact- and machine-specific. The built-in profile starts in auto/off state until the
GGUF probe confirms native MTP heads. On the current RTX 3090, the UI tuner selected `n=3` but rounded
to `1.00x` versus MTP-off, so no material speculative-decoding speedup is claimed.

The 31B context is deliberately conservative for an RTX 3090. Q4/Q5-class profiles are supported;
Q6 is not a default because it does not leave useful KV-cache headroom on 24 GB.

The exact Qwen aliases are sourced from the model repositories rather than guessed from display
names:

- [Unsloth Qwen3.6-27B GGUF](https://huggingface.co/unsloth/Qwen3.6-27B-GGUF)
- [Huihui Qwen3.6-27B abliterated MTP GGUF](https://huggingface.co/huihui-ai/Huihui-Qwen3.6-27B-abliterated-MTP-GGUF)
- [HauhauCS Qwen3.6-27B Aggressive](https://huggingface.co/HauhauCS/Qwen3.6-27B-Uncensored-HauhauCS-Aggressive)
- [llmfan46 Qwen3.6-35B-A3B heretic Native-MTP GGUF](https://huggingface.co/llmfan46/Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved-GGUF)

The template describes a safe starting point, not a quality endorsement. In the 2026-07-21 live
tournament the 35B profile loaded at 16K but failed the required structured-tool gate and is therefore
not a daily-agent recommendation. See `qwen-local-model-tournament-2026-07-21.md`.

## Exact HauhauCS Gemma handling

The exact model card recommends temperature 0.6, top-k 64, top-p 0.9, min-p 0.05, and repeat penalty
1.1. It also ships a separate `mtp-gemma-4-26B-A4B-it.gguf` drafter. Mauler now transports repeat
penalty end-to-end and exposes it in Profiles. It does not enable MTP merely because the repository
name contains `MTP`; the matching draft GGUF must be configured and verified first.

Source: [HauhauCS Gemma4-26B-A4B QAT Balanced MTP model card](https://huggingface.co/HauhauCS/Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-MTP).

## Safe import flow in the UI

1. Open **Control Center > Providers** and list the InferenceBridge models.
2. Under **Profiles**, create/duplicate a profile, use **Pick** (or the UI Model ID field), then click
   **Apply model template**.
3. Review context, thinking, repeat penalty, and any MTP draft path in the same UI.
4. Run **Benchmark LLM**. Apply its recommendation only after checking the live context and protocol
   results.
5. Make the profile the local default only if the benchmark is healthy. Cloud profiles remain
   one-task boosts.

Unknown or renamed fine-tunes may receive conservative family defaults, but remain benchmark-required
until a code-owned exact alias/template is added.
