#!/bin/sh
# Scenario tests for release-plan.sh against isolated local fixture git
# repos and a stubbed gh. Never touches a real repository or publishes a
# real release — this is the mocked git/gh evidence the release handoff
# requires for the workflow's decision table.
#
#   1. neither tag nor release exists          -> tag_and_publish
#   2. tag at HEAD, release missing            -> publish_release_for_existing_tag
#   3. tag at an ancestor, release missing     -> publish_release_for_existing_tag
#   4. tag and release both exist              -> noop
#   5. tag on a foreign commit, no release     -> refusal (exit 2)
#   6. malformed VERSION                       -> refusal (exit 1)
#   7. release lookup fails (auth/API outage)  -> refusal (exit 3, no decision)
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
plan="$here/release-plan.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
out_file="$work/out"

# Stub gh: `release view` answers from FAKE_RELEASE_EXISTS, mirroring real
# gh error strings (verified against gh 2.98: not-found prints "release
# not found"; auth failures print "non-200 OK status code: 401...").
# FAKE_GH_MODE=outage simulates an API/auth failure that is NOT a missing
# release. Any other invocation (e.g. `release create`) is recorded to
# FAKE_GH_LOG and fails the test — the plan script must never publish.
bin="$work/bin"
mkdir -p "$bin"
cat >"$bin/gh" <<'STUB'
#!/bin/sh
if [ "$1" = "release" ] && [ "$2" = "view" ]; then
  if [ "${FAKE_GH_MODE:-}" = "outage" ]; then
    echo "non-200 OK status code: 502 Service Unavailable" >&2
    exit 1
  fi
  if [ "${FAKE_RELEASE_EXISTS:-0}" = "1" ]; then
    exit 0
  fi
  echo "release not found" >&2
  exit 1
fi
printf '%s\n' "$*" >>"${FAKE_GH_LOG:?}"
echo "unexpected gh invocation: $*" >&2
exit 3
STUB
chmod +x "$bin/gh"

pass=0
fail=0

assert_eq() { # assert_eq <description> <expected> <actual>
  if [ "$2" = "$3" ]; then
    pass=$((pass + 1))
    printf 'ok   %s\n' "$1"
  else
    fail=$((fail + 1))
    printf 'FAIL %s: expected <%s>, got <%s>\n' "$1" "$2" "$3" >&2
  fi
}

field() { # field <key> — reads the last plan output
  sed -n "s/^$1=//p" "$out_file" | head -1
}

new_repo() { # new_repo <dir> <version>
  mkdir -p "$1"
  (
    cd "$1"
    git init -q -b main .
    git config user.email test@example.com
    git config user.name "Fixture"
    printf '%s\n' "$2" > VERSION
    echo one > file
    git add .
    git commit -qm "c1"
    echo two >> file
    git commit -qam "c2"
  )
}

run_plan() { # run_plan <repo-dir> [release_exists:0|1] [gh_mode]; exit code lands in STATUS
  STATUS=0
  (
    cd "$1"
    PATH="$bin:$PATH"
    FAKE_GH_LOG="$work/gh.log"
    FAKE_RELEASE_EXISTS="${2:-0}"
    FAKE_GH_MODE="${3:-}"
    export PATH FAKE_GH_LOG FAKE_RELEASE_EXISTS FAKE_GH_MODE
    sh "$plan" 2>/dev/null
  ) >"$out_file" || STATUS=$?
}

# 1. No tag, no release: normal publish path.
r="$work/case1"; new_repo "$r" 1.6.0
run_plan "$r"
assert_eq "case1 exit code" 0 "$STATUS"
assert_eq "case1 decision" "tag_and_publish" "$(field decision)"
assert_eq "case1 tag" "v1.6.0" "$(field tag)"

# 2. Tag at HEAD (tag push landed, release creation failed), no release.
r="$work/case2"; new_repo "$r" 1.6.0
git -C "$r" tag -a v1.6.0 -m fixture
run_plan "$r"
assert_eq "case2 exit code" 0 "$STATUS"
assert_eq "case2 decision" "publish_release_for_existing_tag" "$(field decision)"

# 3. Tag at an ancestor (later merges without a bump, release record
#    missing): recover the tagged artifact.
r="$work/case3"; new_repo "$r" 1.6.0
git -C "$r" tag -a v1.6.0 -m fixture HEAD~1
echo three >>"$r/file"; git -C "$r" commit -qam "c3"
run_plan "$r"
assert_eq "case3 exit code" 0 "$STATUS"
assert_eq "case3 decision" "publish_release_for_existing_tag" "$(field decision)"

# 4. Tag and release both exist: merge without a bump is a no-op.
r="$work/case4"; new_repo "$r" 1.6.0
git -C "$r" tag -a v1.6.0 -m fixture
run_plan "$r" 1
assert_eq "case4 exit code" 0 "$STATUS"
assert_eq "case4 decision" "noop" "$(field decision)"

# 5. Tag exists on a commit that is neither HEAD nor an ancestor: refuse.
r="$work/case5"; new_repo "$r" 1.6.0
(
  cd "$r"
  git switch -q -c other
  git commit -q --allow-empty -m "foreign"
  git tag -a v1.6.0 -m fixture
  git switch -q main
)
run_plan "$r"
assert_eq "case5 exit code" 2 "$STATUS"
assert_eq "case5 emits no decision" "" "$(field decision)"

# 6. Malformed VERSION: refuse before touching git or gh.
r="$work/case6"; new_repo "$r" next
run_plan "$r"
assert_eq "case6 exit code" 1 "$STATUS"

# 7. Release lookup fails with a non-404 error (auth/API outage): an
#    unknown release state must abort without a decision — never treated
#    as a missing release that triggers a recovery publish.
r="$work/case7"; new_repo "$r" 1.6.0
git -C "$r" tag -a v1.6.0 -m fixture
run_plan "$r" 0 outage
assert_eq "case7 exit code" 3 "$STATUS"
assert_eq "case7 emits no decision" "" "$(field decision)"

# The plan must be read-only: no gh write invocation in any scenario.
assert_eq "no gh write invocations" "" "$(cat "$work/gh.log" 2>/dev/null || true)"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
