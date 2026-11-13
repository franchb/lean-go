# Extraction diagnostics

Diagnostics are **post-alpha.4 development** functionality in `0.1.0a5.dev0`.
The published `v0.1.0-alpha.4` installation does not include this interface.
Use a checkout of this development revision and `uv sync --locked` (refresh an
existing editable installation with `uv sync --locked --reinstall-package lean-go-tools`).
The pinned Python 3.15, Go 1.27.4 and Lean 4.36.0-rc2 toolchains remain required.

Generation prints readable errors to stderr by default, including a known LCNF
form, the original rejection reason and a next-step hint. Select one JSON report
with either `--diagnostics=json` or `--diagnostics json`:

```sh
uv run --locked lean-go generate \
  --source-dir ./source --module Example --root Example.run \
  --out ./generated/example --ir-output ./example.ir.json \
  --diagnostics=json
```

The same option works on `scripts/generate.py` and `scripts/export_module.py`.
Failures return a nonzero exit status and print one JSON document on stderr,
without additional child-process logs or a second generic error. Child failure
logs may appear inside the report's escaped `message` string. Failure produces
no stdout. Successful generation retains its human-readable stdout summary;
successful `export_module.py` is silent. Success does not emit a JSON report.
Invalid argparse syntax (for example a missing flag value or an unknown option)
still prints argparse usage text and exits with status 2. Extraction errors,
missing toolchains, invalid source/output paths and installed-CLI preflight
errors use the selected diagnostic format.

An unsupported polymorphic function can produce:

```json
{"schema":"lean-go-diagnostics/v1","success":false,"diagnostics":[{"code":"unsupported_type","stage":"admission","message":"Unsupported first-order monomorphic type: Type","declaration":"Example.run","compiler_form":null,"location":{"path":"Example.lean","kind":"declaration","declaration":"Example.run","start":{"line":2,"column":4,"utf16_column":4,"byte_offset":22},"end":{"line":2,"column":7,"utf16_column":7,"byte_offset":25}},"hint":"Use a concrete first-order data type; polymorphism and function-valued types are outside this profile."}]}
```

The envelope and field types are defined by the
[JSON schema](diagnostics.schema.json). The message preserves the producer's
rejection reason. Match `stage` and `code` in automation, rather than English
message text. Unknown declaration, compiler form or location is JSON `null`.
Codes and compiler forms originate at explicit producer checks; they are never
inferred from message text or generated-name suffixes.

| Stage | Meaning | Codes |
| --- | --- | --- |
| `input` | Invalid arguments, paths or source snapshot | `invalid_input` |
| `toolchain` | Missing or mismatched required tools | `toolchain_unavailable`, `toolchain_mismatch`, `toolchain_failed` |
| `build` | Compilation, subprocess, or producer protocol failure | `producer_failed`, `producer_protocol`, `build_failed`, `pipeline_failed` |
| `admission` | An explicit supported-profile rejection | `source_audit`, `unsupported_type`, `unsupported_signature`, `unsupported_lcnf_value`, `unsupported_lcnf_control` |
| `emission` | Go generation or publishing output failed | `emission_failed`, `output_failed` |

A compiler crash, syntax error, failed producer build, or unclassified compiler
exception is a build failure. It must **never count as an expected admission
rejection** in compatibility tests. Other existing invariant failures may also
remain build failures until their deliberate rejection site supplies a code.
The fixed audited normalizer uses the build-failure fallback; the fine-grained
admission codes describe ordinary module extraction.

Locations refer to original files relative to the source snapshot. Lean's
imported declaration metadata supplies the name's selection range. For generated
helpers lacking their own range, validated compiler ownership may supply a
`source_owner` range; `location.declaration` then names that owner while the
outer `declaration` remains the failing helper. Missing ownership or range data
stays null. A compiler position prefix gives a `compiler` location with a
zero-width range and unknown declaration; this is only used for a path verified
against the fresh source workspace.

Lines start at 1. `column` counts Unicode code points from 0; `utf16_column`
counts UTF-16 code units from 0 for editor integration. `byte_offset` is the
UTF-8 offset from the original file start. Range ends are exclusive. Only LF
advances lines; CR remains a character. Mapping uses the exact byte position and
length of the instrumentation insertion, including same-line imports, Unicode
and CRLF. Locations inside injected code or outside the original snapshot stay
null.

Diagnostics do not relax admission or change runtime behavior. Failed source
admission preserves existing package and IR output. The installed CLI continues
to copy trusted assets to a temporary build workspace and leaves installed files
unchanged. Producer and proof-checker identities include their consumed diagnostic
helpers; regenerate artifacts and proof evidence after changing these inputs.
