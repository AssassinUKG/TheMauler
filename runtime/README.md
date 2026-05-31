# TheMauler Runtime Registry

This directory records reproducible local-model runtime facts: model families,
templates, launch profiles, benchmark results, and lock snapshots.

The application currently ships built-in runtime profiles in Go code so Doctor
and task logs can use them without depending on external files. The JSON files
here mirror that intent and provide the place to pin future model/template
hashes, llama.cpp commits, CUDA versions, and benchmark output.

Suggested layout:

- `models/`: model-family capability records.
- `templates/`: model/chat-template notes and hashes.
- `profiles/`: known-good launch/profile presets.
- `benchmarks/`: eval and speed results captured after runtime changes.

The live runtime lock for the last run is written to:

```text
~/.config/mauler/runtime-lock.json
```
