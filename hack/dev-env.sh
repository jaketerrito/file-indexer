#!/usr/bin/env bash
# Print export lines pinning this checkout's dev environment:
# WORKTREE_INDEX, K8S_CONTEXT, K8S_NAMESPACE, TILT_PORT.
# Usage: eval "$(hack/dev-env.sh)"
#
# Derivation (deterministic from the checkout path, no state files):
# - index: 0 for a plain checkout, else sha1(root)[:8] as hex % 100 + 1
# - namespace: "default" for index 0, else wt-<slug>-<sha1(root)[:6]>
# - Tilt UI port: 10350 + index (the only per-checkout host port)
# Overrides: WORKTREE_INDEX, K8S_NAMESPACE, K8S_CONTEXT, TILT_PORT.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
git_dir="$(realpath "$(git rev-parse --git-dir)")"
common="$(realpath "$(git rev-parse --git-common-dir)")"
hash="$(printf %s "$root" | sha1sum | cut -c1-8)"

# A plain checkout's git dir IS the common dir; a linked worktree's git dir
# lives under <main>/.git/worktrees/<name>.
index=0
if [[ "$git_dir" != "$common" ]]; then
    index=$(( $(printf '%d' "0x$hash") % 100 + 1 ))
fi
if [[ -n "${WORKTREE_INDEX:-}" ]]; then
    index="$WORKTREE_INDEX"
fi

namespace="default"
if (( index != 0 )); then
    # slug: lowercase, non [a-z0-9-] → '-', collapse runs of '-', strip
    # leading/trailing '-', cut to 20 chars.
    slug="$(basename "$root" | tr 'A-Z' 'a-z' \
        | sed -e 's/[^a-z0-9-]/-/g' -e 's/-\{2,\}/-/g' -e 's/^-//' -e 's/-$//' \
        | cut -c1-20)"
    namespace="wt-$slug-${hash:0:6}"
fi
if [[ -n "${K8S_NAMESPACE:-}" ]]; then
    namespace="$K8S_NAMESPACE"
fi

printf 'export WORKTREE_INDEX=%s\n' "$index"
printf 'export K8S_CONTEXT=%s\n' "${K8S_CONTEXT:-kind-kind}"
printf 'export K8S_NAMESPACE=%s\n' "$namespace"
printf 'export TILT_PORT=%s\n' "${TILT_PORT:-$((10350 + index))}"
