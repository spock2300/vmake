#!/usr/bin/env bash
set -euo pipefail

# Automated skill optimization loop.
#
# Roles:
#   solver  - fresh vmake user; only sees the installed skill and a temp project
#   judge   - rubric evaluator for knowledge tasks
#   author  - edits a disposable copy of the repository; only the skill dir is applied back
#
# The controller (this script) is the scheduler/evaluator. It keeps a candidate
# only when it improves the score, and stops when all tasks pass or the score
# stops improving.

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
SKILL="$ROOT/cmd/vmake/skills/vmake"
MODEL=${VMAKE_SKILL_MODEL:-deepseek/deepseek-flash}
MAX_ROUNDS=${VMAKE_SKILL_ROUNDS:-3}
KEEP=${VMAKE_SKILL_KEEP:-0}
TASKS=${VMAKE_SKILL_TASKS:-"compiler multi platform doctor-json install-manifest onrequire public buildkey dryrun targets options"}
WORK=${VMAKE_SKILL_WORK:-$(mktemp -d /tmp/vmake-skill-loop.XXXXXX)}
TC_NAME=${VMAKE_SKILL_TC_NAME:-skill-loop-tc-$$}
VMAKE="$WORK/bin/vmake"
OPENCODE_BIN=${OPENCODE_BIN:-$(command -v opencode || true)}

TASK_PASS=0
TASK_REASON=""
TASK_VIOLATIONS=0

log()  { printf '[skill-loop] %s\n' "$*"; }
die()  { printf '[skill-loop] error: %s\n' "$*" >&2; exit 1; }

cleanup() {
  local status=$?
  if [ -x "$VMAKE" ]; then
    "$VMAKE" ext remove "$TC_NAME" >/dev/null 2>&1 || true
    ( cd "$ROOT" && CGO_ENABLED=0 go build -o "$VMAKE" ./cmd/vmake >/dev/null 2>&1 ) || true
    "$VMAKE" skill install >/dev/null 2>&1 || true
  fi
  if [ "$KEEP" != "1" ]; then
    chmod -R u+w "$WORK" 2>/dev/null || true
    rm -rf "$WORK"
  else
    log "work directory kept at $WORK"
  fi
  exit "$status"
}

trap cleanup EXIT

[ -n "$OPENCODE_BIN" ] || die "opencode not found in PATH"
command -v jq >/dev/null || die "jq is required"
command -v git >/dev/null || die "git is required"
command -v go >/dev/null || die "go is required"

mkdir -p "$WORK/bin" "$WORK/tc/bin" "$WORK/tc/repo/tc"
for tool in gcc g++ ar ld; do
  src=$(command -v "$tool" || true)
  [ -n "$src" ] || die "host tool $tool not found"
  ln -sf "$src" "$WORK/tc/bin/$tool"
done
cat > "$WORK/tc/repo/tc/toolchain.json" <<EOF
{
  "name": "$TC_NAME",
  "display_name": "Skill Loop Toolchain",
  "tools": {
    "cc": "$WORK/tc/bin/gcc",
    "cxx": "$WORK/tc/bin/g++",
    "ar": "$WORK/tc/bin/ar",
    "ld": "$WORK/tc/bin/ld"
  }
}
EOF
git -C "$WORK/tc/repo" init -q
git -C "$WORK/tc/repo" add .
git -C "$WORK/tc/repo" -c user.email=skill-loop@example.com -c user.name=skill-loop commit -qm init

( cd "$ROOT" && CGO_ENABLED=0 go build -o "$VMAKE" ./cmd/vmake )
"$VMAKE" ext remove "$TC_NAME" >/dev/null 2>&1 || true
"$VMAKE" -y ext add "$TC_NAME" "$WORK/tc/repo" >/dev/null

run_agent() {
  local dir=$1 title=$2 prompt=$3 out=$4
  ( cd "$dir" && "$OPENCODE_BIN" run --format json --auto --model "$MODEL" \
      --title "$title" "$(cat "$prompt")" ) >"$out" 2>"$out.err"
}

extract_text() {
  local json=$1 out=$2
  jq -r 'select(.type=="text") | .part.text' "$json" >"$out" 2>/dev/null || true
}

extract_commands() {
  local json=$1 out=$2
  jq -r 'select(.type=="tool_use" and (.part.tool=="shell" or .part.tool=="bash")) | .part.state.input.command // empty' \
    "$json" >"$out" 2>/dev/null || true
}

count_violations() {
  local cmds=$1
  if grep -Eq '(^|[;&|[:space:]])(export[[:space:]]+)?(PATH|CC|CXX|AR|LD)=' "$cmds" \
     || grep -Eq 'export[[:space:]]+(PATH|CC|CXX|AR|LD)\b' "$cmds" \
     || grep -Eq '>>?[[:space:]]*(~|\$HOME)/\.(bashrc|zshrc|profile)' "$cmds" \
     || grep -Fq "$ROOT" "$cmds"; then
    echo 1
  else
    echo 0
  fi
}

fail_task() { TASK_PASS=0; TASK_REASON=$1; TASK_VIOLATIONS=${2:-0}; }
pass_task() { TASK_PASS=1; TASK_REASON=""; TASK_VIOLATIONS=${1:-0}; }

task_compiler() {
  local dir=$1
  rm -rf "$dir"
  mkdir -p "$dir"
  cp -a "$ROOT/test_data/01_simple_c/." "$dir/project/"
  rm -rf "$dir/project/.vmake"
  if ! ( cd "$dir/project" && "$VMAKE" config --set toolchain="$TC_NAME" >/dev/null 2>&1 ); then
    fail_task "could not configure the project toolchain"; return
  fi
  if ! run_agent "$dir/project" "skill-loop compiler" "$HERE/prompts/compiler.md" "$dir/transcript.json"; then
    fail_task "solver failed to run"; return
  fi
  extract_commands "$dir/transcript.json" "$dir/commands.txt"
  extract_text "$dir/transcript.json" "$dir/answer.txt"
  local violations
  violations=$(count_violations "$dir/commands.txt")
  if [ "$violations" -gt 0 ]; then
    fail_task "solver mutated PATH/CC/... or read the vmake source repo" "$violations"; return
  fi
  if ! jq -e '.status == "succeeded"' "$dir/project/build/vmake-build.json" >/dev/null 2>&1; then
    fail_task "build report is not succeeded"; return
  fi
  local bin
  bin=$(find "$dir/project/build" -maxdepth 2 -type f \( -name hello -o -name hello.exe \) 2>/dev/null | head -1)
  if [ -z "$bin" ] || [ ! -x "$bin" ] || ! "$bin" 2>/dev/null | grep -q 'Hello, VMake!'; then
    fail_task "hello binary was not built or did not run"; return
  fi
  if ! grep -Fq "$WORK/tc/bin/gcc" "$dir/answer.txt" && ! grep -Fq '/usr/bin/gcc' "$dir/answer.txt"; then
    fail_task "answer did not report the compiler path"; return
  fi
  pass_task "$violations"
}

task_multi() {
  local dir=$1
  rm -rf "$dir"
  mkdir -p "$dir"
  cp -a "$ROOT/test_data/03_multi_target/." "$dir/project/"
  rm -rf "$dir/project/.vmake"
  if ! run_agent "$dir/project" "skill-loop multi-target" "$HERE/prompts/multi-target.md" "$dir/transcript.json"; then
    fail_task "solver failed to run"; return
  fi
  extract_commands "$dir/transcript.json" "$dir/commands.txt"
  extract_text "$dir/transcript.json" "$dir/answer.txt"
  local violations
  violations=$(count_violations "$dir/commands.txt")
  if [ "$violations" -gt 0 ]; then
    fail_task "solver mutated PATH/CC/... or read the vmake source repo" "$violations"; return
  fi
  if ! jq -e '.status == "succeeded"' "$dir/project/build/vmake-build.json" >/dev/null 2>&1; then
    fail_task "build report is not succeeded"; return
  fi
  if ! jq -e '.status == "succeeded" and ((.tests // []) | length) > 0 and ([.tests[]?.passed] | all)' \
      "$dir/project/build/vmake-test.json" >/dev/null 2>&1; then
    fail_task "test report is missing or not succeeded"; return
  fi
  pass_task "$violations"
}

run_judge() {
  local dir=$1 answer=$2 rubric=$3 out_json=$4 out_text=$5
  mkdir -p "$dir"
  {
    cat "$rubric"
    printf '\n<<<ANSWER>>>\n'
    cat "$answer"
    printf '\n<<<END>>>\n'
  } > "$dir/prompt.md"
  if ! run_agent "$dir" "skill-loop judge" "$dir/prompt.md" "$out_json"; then
    return 1
  fi
  extract_text "$out_json" "$out_text"
  grep -Eq '^[[:space:]]*PASS' "$out_text"
}

task_knowledge() {
  local dir=$1 name=$2
  local prompt="$HERE/prompts/$name.md"
  local rubric="$HERE/prompts/judge-$name.md"
  rm -rf "$dir"
  mkdir -p "$dir"
  if [ ! -f "$prompt" ] || [ ! -f "$rubric" ]; then
    fail_task "missing prompt or rubric for $name"; return
  fi
  if ! run_agent "$dir" "skill-loop $name" "$prompt" "$dir/transcript.json"; then
    fail_task "solver failed to run"; return
  fi
  extract_commands "$dir/transcript.json" "$dir/commands.txt"
  extract_text "$dir/transcript.json" "$dir/answer.txt"
  local violations
  violations=$(count_violations "$dir/commands.txt")
  if [ "$violations" -gt 0 ]; then
    fail_task "solver mutated PATH/CC/... or read the vmake source repo" "$violations"; return
  fi
  if ! run_judge "$dir/judge" "$dir/answer.txt" "$rubric" "$dir/judge.json" "$dir/judge.txt"; then
    fail_task "$name rubric failed: $(head -1 "$dir/judge.txt" 2>/dev/null)"; return
  fi
  pass_task "$violations"
}

run_round() {
  local src=$1 label=$2
  local rd="$WORK/rounds/$label"
  mkdir -p "$rd"
  log "round $label: build and install skill from $src"
  if ! ( cd "$src" && CGO_ENABLED=0 go build -o "$VMAKE" ./cmd/vmake ); then
    die "build failed for $src"
  fi
  "$VMAKE" skill install >/dev/null

  local passes=0 total=0 violations=0
  : > "$rd/summary.md"
  {
    printf '# Round %s\n\n' "$label"
  } >> "$rd/summary.md"

  for task in $TASKS; do
    total=$((total + 1))
    case "$task" in
      compiler|multi) "task_$task" "$rd/$task" ;;
      *) task_knowledge "$rd/$task" "$task" ;;
    esac
    passes=$((passes + TASK_PASS))
    violations=$((violations + TASK_VIOLATIONS))
    if [ "$TASK_PASS" -eq 1 ]; then
      printf -- '- %s: PASS\n' "$task" >> "$rd/summary.md"
    else
      {
        printf -- '- %s: FAIL: %s\n' "$task" "$TASK_REASON"
        printf '\n  Solver answer:\n\n```text\n'
        head -40 "$rd/$task/answer.txt" 2>/dev/null || true
        printf '```\n\n  Commands:\n\n```text\n'
        head -40 "$rd/$task/commands.txt" 2>/dev/null || true
        printf '```\n\n'
      } >> "$rd/summary.md"
    fi
  done

  local words
  words=$(find "$src/cmd/vmake/skills/vmake" -type f -name '*.md' -exec cat {} + | wc -w | tr -d ' ')
  {
    printf '\nScore: %d/%d passes, %d violation(s), %s words\n' "$passes" "$total" "$violations" "$words"
  } >> "$rd/summary.md"
  printf '%s\t%s\t%s\t%s\n' "$label" "$passes" "$violations" "$words" >> "$WORK/scores.tsv"
  log "round $label: $passes/$total passes, $violations violation(s), $words words"
  ROUND_PASSES=$passes
  ROUND_TOTAL=$total
  ROUND_VIOLATIONS=$violations
  ROUND_WORDS=$words
  ROUND_SUMMARY="$rd/summary.md"
}

better_than() {
  local p=$1 v=$2 w=$3 bp=$4 bv=$5 bw=$6
  [ "$p" -gt "$bp" ] && return 0
  [ "$p" -eq "$bp" ] && [ "$v" -lt "$bv" ] && return 0
  [ "$p" -eq "$bp" ] && [ "$v" -eq "$bv" ] && [ "$w" -lt "$bw" ] && return 0
  return 1
}

author_round() {
  local round=$1 feedback=$2
  local author="$WORK/author-$round"
  log "author round $round: creating isolated repository copy"
  rm -rf "$author"
  mkdir -p "$author"
  rsync -a --delete \
    --exclude .git --exclude .vmake --exclude .vmake_deps --exclude .gocache \
    --exclude /build --exclude /install --exclude /bin \
    --exclude /vmake --exclude /vmake.exe \
    "$ROOT/" "$author/"

  {
    cat "$HERE/prompts/author.md"
    printf '\n## Feedback from the verifier\n\n'
    cat "$feedback"
    printf '\n## Repository copy\n\n%s\n' "$author"
  } > "$WORK/author-prompt-$round.md"

  if ! run_agent "$author" "skill-loop author $round" "$WORK/author-prompt-$round.md" "$WORK/author-$round.json"; then
    log "author failed to run"
    return 1
  fi
  extract_text "$WORK/author-$round.json" "$WORK/author-$round.txt"

  if ! ( cd "$author" && CGO_ENABLED=0 go test ./cmd/vmake -run '^TestSkill' -count=1 >"$WORK/author-$round-test.log" 2>&1 ); then
    log "author broke TestSkill; candidate rejected"
    tail -20 "$WORK/author-$round-test.log" >&2 || true
    return 1
  fi
  if ! diff -qr "$SKILL" "$author/cmd/vmake/skills/vmake" >/dev/null 2>&1; then
    log "author changed the skill directory"
    return 0
  fi
  log "author made no skill changes"
  return 1
}

# --- main ---------------------------------------------------------------

run_round "$ROOT" baseline
BEST_PASSES=$ROUND_PASSES
BEST_VIOLATIONS=$ROUND_VIOLATIONS
BEST_WORDS=$ROUND_WORDS
BEST_TOTAL=$ROUND_TOTAL
BEST_SUMMARY=$ROUND_SUMMARY

if [ "$BEST_PASSES" -eq "$BEST_TOTAL" ] && [ "$BEST_VIOLATIONS" -eq 0 ]; then
  log "baseline already passes every task; nothing to optimize"
  cat "$BEST_SUMMARY"
  exit 0
fi

mkdir -p "$WORK/best-skill"
rsync -a --delete "$SKILL/" "$WORK/best-skill/"

for round in $(seq 1 "$MAX_ROUNDS"); do
  if ! author_round "$round" "$BEST_SUMMARY"; then
    log "no accepted author change in round $round; stopping"
    break
  fi
  run_round "$WORK/author-$round" "round-$round"
  if better_than "$ROUND_PASSES" "$ROUND_VIOLATIONS" "$ROUND_WORDS" \
                 "$BEST_PASSES" "$BEST_VIOLATIONS" "$BEST_WORDS"; then
    log "candidate improved the score; applying it to the repository"
    rsync -a --delete "$WORK/author-$round/cmd/vmake/skills/vmake/" "$SKILL/"
    BEST_PASSES=$ROUND_PASSES
    BEST_VIOLATIONS=$ROUND_VIOLATIONS
    BEST_WORDS=$ROUND_WORDS
    BEST_TOTAL=$ROUND_TOTAL
    BEST_SUMMARY=$ROUND_SUMMARY
    if [ "$BEST_PASSES" -eq "$BEST_TOTAL" ] && [ "$BEST_VIOLATIONS" -eq 0 ]; then
      log "all tasks pass; converged"
      break
    fi
  else
    log "candidate did not improve the score; reverting"
    rsync -a --delete "$WORK/best-skill/" "$SKILL/"
    ( cd "$ROOT" && CGO_ENABLED=0 go build -o "$VMAKE" ./cmd/vmake >/dev/null )
    "$VMAKE" skill install >/dev/null
    break
  fi
done

{
  printf '# Skill loop results\n\n'
  printf '| round | passes | violations | words |\n'
  printf '| --- | --- | --- | --- |\n'
  awk -F '\t' '{printf "| %s | %s | %s | %s |\n", $1, $2, $3, $4}' "$WORK/scores.tsv"
} > "$WORK/results.md"

cat "$WORK/results.md"
log "best score: $BEST_PASSES/$BEST_TOTAL passes, $BEST_VIOLATIONS violation(s), $BEST_WORDS words"
if [ "$KEEP" = "1" ]; then
  log "artifacts: $WORK"
fi
