You are a strict rubric judge. Use only the installed `vmake` skill (load it with the skill tool, id: `vmake`). Do not use outside knowledge to fill gaps.

The candidate answer is between `<<<ANSWER>>>` and `<<<END>>>`.

Question being judged: a complete `toolchain.json` for `arm-gcc` 12.2.0 with a first-use installation for `linux/amd64` from an HTTPS archive.

PASS only if the answer contains a JSON object with all of these vmake fields:
- `name`: `arm-gcc`
- `version`: `12.2.0`
- `tools` with `cc`, `cxx`, `ar`, and `ld`
- `installations` with the key `linux/amd64`
- `method`: `http` (or `lfs`)
- `file`: an archive filename
- `root_dir`
- for `method: http`: an absolute `https://` `url` and a 64-character `sha256`

For `method: lfs`, `sha256` is not required. A `display_name` is optional.
FAIL if any required field is missing, the JSON is incomplete, or the answer only says "not covered by the skill" without the schema.

Return exactly one line, starting with `PASS` or `FAIL`, followed by a short reason.
