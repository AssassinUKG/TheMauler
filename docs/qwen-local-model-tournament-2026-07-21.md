# Qwen local-model tournament — 2026-07-21

## Outcome

`Qwen3.6-27B-UD-Q4_K_XL.gguf` is the best installed daily Qwen for Mauler's current local setup.
It combines the strongest strict-suite average (54.8 tok/s), a 90/100 suite score, a clean required
native tool call with zero repair, and a passing six-tool mini agent loop.

It is now both the current installation's persistent local default and the code-owned default model
for fresh Mauler configurations. Fresh profiles use the stable `qwen3.6-nothink` name; the current
installation retains its already-verified `qwen3.6-nothink-copy` profile rather than silently
renaming or deleting user configuration.

`Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf` is the close alternative. It won the initial
text-speed row and tied the best loop score, but averaged 51.5 tok/s in the four-scenario suite.

The 35B-A3B model is not recommended for daily Mauler work on this RTX 3090 configuration. It was
10.9 tok/s, did not produce the required structured tool call, and failed the mini loop.

## Fair matrix

All six models used the same Qwen no-thinking sampling defaults and one 16,384-token matrix context.
Each row loaded the exact provider model, verified actual context, generated a 256-token response,
attempted a required native tool call, then ran the same bounded read/write/verify mini loop.

| Model | Text tok/s | Required tool | Mini loop | Loop time |
|---|---:|---|---|---:|
| Qwen3.6 27B Fable/Fus Q4_K_M | 33.2 | pass, 0 repairs | fail, score 24, 83% steps | 107 s |
| Qwen3.6 27B HauhauCS Aggressive Q4_K_P | 33.2 | pass, 0 repairs | pass, score 36, 83% steps | 104 s |
| Huihui Qwen3.6 27B abliterated Q4_K | **52.6** | pass, 0 repairs | **pass, score 43**, 83% steps | **95 s** |
| Tess-4 27B Q4_K_M | 34.2 | pass, 0 repairs | **pass, score 43**, 83% steps | 96 s |
| Qwen3.6 35B-A3B heretic Native-MTP Q4_K_M | 10.9 | **fail** | **fail, score 0**, 50% steps | 118 s |
| Qwen3.6 27B Unsloth UD-Q4_K_XL | 51.3 | pass, 0 repairs | **pass, score 43**, 83% steps | 96 s |

The matrix reports two failures because Fable missed the loop threshold and the 35B missed both the
required-tool and loop gates. Five of six models passed the direct native-tool protocol check.

## Finalist Advanced Suite

Both finalists ran the same four scenarios at a 35,000-token profile context: text, TypeScript coding,
strict JSON discipline, and required tool calling.

| Scenario | Huihui Q4_K | Unsloth UD-Q4_K_XL |
|---|---:|---:|
| Text | 45.5 tok/s | 42.4 tok/s |
| Coding | 56.5 tok/s | **58.2 tok/s** |
| JSON discipline | 46.1 tok/s | **55.8 tok/s** |
| Required tool | 58.1 tok/s, 1 structured, 0 repaired | **62.6 tok/s**, 1 structured, 0 repaired |
| Overall | 90/100, 51.5 tok/s | **90/100, 54.8 tok/s** |

The first scenario includes cold/load overhead, so its TTFT is not directly comparable with later
warm scenarios. The aggregate and protocol results are the useful decision signals.

## Grammar-constrained caveat

The separate grammar probe for the winning UD profile returned `HOLD`: no structured `tool_calls`
entry, invalid constrained arguments, and backend support reported `no`. This conflicts with the clean
ordinary OpenAI tool call in both the matrix and Advanced Suite, so it is treated as a constrained-
grammar/backend compatibility gap rather than proof that normal tool calls are broken.

Consequences:

- Good for supervised chat, coding, JSON, and normal Mauler tool loops.
- Do not label it fully unattended-safe while the grammar probe is red.
- Keep Mauler's schema repair, verification, budgets, and control-plane gates enabled.
- Re-run the grammar probe after InferenceBridge/llama.cpp grammar support changes.

## Saved settings

The winning UI profile uses:

| Setting | Value |
|---|---:|
| Context | 35,000 |
| Thinking | off |
| Temperature | 0.2 |
| Top P | 0.95 |
| Top K | 20 |
| Min P | 0 |
| Presence penalty | 0 |
| Repeat penalty | 1.05 |
| Max output | 8,192 |
| MTP speculative decoding | native `draft-mtp`, calibrated `n=3` |

These are now code-owned in the exact Unsloth UD template, along with exact templates for the Huihui,
HauhauCS Aggressive, and installed 35B-A3B names.

After the winner became the local default, Mauler's GGUF probe enabled native self-drafting and the
built-in `n=1..5` tuner selected `n=3`. The measured result rounded to `1.00x` versus MTP-off, so this
is not claimed as a material speed improvement; it is simply the fastest setting in the local sweep.
No external draft-model path is used.

## Source identity

- [Unsloth Qwen3.6-27B GGUF and UD-Q4_K_XL artifact](https://huggingface.co/unsloth/Qwen3.6-27B-GGUF)
- [Huihui abliterated MTP GGUF](https://huggingface.co/huihui-ai/Huihui-Qwen3.6-27B-abliterated-MTP-GGUF)
- [HauhauCS Aggressive model card](https://huggingface.co/HauhauCS/Qwen3.6-27B-Uncensored-HauhauCS-Aggressive)
- [35B-A3B heretic Native-MTP GGUF](https://huggingface.co/llmfan46/Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved-GGUF)

"Unrestricted" describes refusal tuning; it does not prove coding quality, tool reliability, or safe
autonomy. Those properties are judged by the local evidence above.
