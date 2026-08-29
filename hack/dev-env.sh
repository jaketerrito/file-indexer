#!/usr/bin/env bash
# dev-env.sh — print `export KEY=value` lines pinning this checkout's dev
# environment: the shared cluster's context, this checkout's namespace, and
# every host port Tilt forwards or tests connect to. Sourced by the just
# recipes via `eval "$(hack/dev-env.sh)"`.
#
# All checkouts share ONE kind cluster ("kind", context "kind-kind") and one
# image registry, so a laptop can run many worktrees at once without paying
# for a control plane per checkout. Isolation is per-namespace:
#
#   main checkout  → namespace "default", ports 10350/3000/5432/9000/9001/
#                    50052/50053 (the historical values; identical to CI)
#   git worktree   → namespace "wt-<slug>-<hash>", every port shifted by a
#                    stable WORKTREE_INDEX derived from the worktree path
#
# Port formula (N = WORKTREE_INDEX): Tilt UI 10350+N, web 3000+N,
# postgres 5432+N, MinIO 9000+2N / 9001+2N, gRPC 50052+2N / 50053+2N.
# The stride of 2 keeps the MinIO API/console and gRPC pairs from colliding
# across adjacent indexes.
#
# Usage:
#   eval "$(hack/dev-env.sh)"   — export the environment
#   hack/dev-env.sh --check     — exit 1 if any of this checkout's ports is
#                                 already listening (another checkout may be
#                                 up; override with WORKTREE_INDEX=<n>)
#
# Overrides: WORKTREE_INDEX=<n> forces the port index; K8S_NAMESPACE,
# TILT_PORT, WEB_PORT, DB_PORT, S3_PORT, S3_CONSOLE_PORT, FILES_PORT,
# SEARCH_PORT can each be set explicitly and are left untouched.

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
git_dir="$(realpath "$(git rev-parse --git-dir)")"
common_dir="$(realpath "$(git rev-parse --git-common-dir)")"

# A plain checkout's git dir IS the common dir; a linked worktree's git dir
# lives under <main>/.git/worktrees/<name>.
if [[ "$git_dir" == "$common_dir" ]]; then
	default_index=0
else
	hash="$(printf '%s' "$root" | sha1sum | cut -c1-8)"
	default_index=$((16#$hash % 100 + 1))
fi
index="${WORKTREE_INDEX:-$default_index}"

# The main checkout deploys to "default" so local runs match CI exactly;
# worktrees get their own namespace named after the worktree directory.
if [[ "$index" == 0 ]]; then
	default_namespace=default
else
	slug="$(basename "$root" |
		tr '[:upper:]' '[:lower:]' |
		sed -e 's/[^a-z0-9-]/-/g' -e 's/-\{2,\}/-/g' -e 's/^-//' -e 's/-$//' |
		cut -c1-20)"
	default_namespace="wt-${slug}-$(printf '%s' "$root" | sha1sum | cut -c1-6)"
fi

TILT_PORT="${TILT_PORT:-$((10350 + index))}"
WEB_PORT="${WEB_PORT:-$((3000 + index))}"
DB_PORT="${DB_PORT:-$((5432 + index))}"
S3_PORT="${S3_PORT:-$((9000 + 2 * index))}"
S3_CONSOLE_PORT="${S3_CONSOLE_PORT:-$((9001 + 2 * index))}"
FILES_PORT="${FILES_PORT:-$((50052 + 2 * index))}"
SEARCH_PORT="${SEARCH_PORT:-$((50053 + 2 * index))}"

if [[ "${1:-}" == "--check" ]]; then
	# The preflight exists to stop concurrent local checkouts from fighting
	# over ports. CI runners are single-tenant, and an unrelated port being
	# bound there (the GitHub images ship postgres/mysql) must not fail a
	# build, so skip it.
	if [[ -n "${CI:-}" ]]; then
		exit 0
	fi
	busy=()
	for port in "$TILT_PORT" "$WEB_PORT" "$DB_PORT" "$S3_PORT" \
		"$S3_CONSOLE_PORT" "$FILES_PORT" "$SEARCH_PORT"; do
		if ss -tlnH "sport = :$port" | grep -q .; then
			busy+=("$port")
		fi
	done
	if ((${#busy[@]})); then
		echo "dev-env: port(s) already in use: ${busy[*]}" >&2
		echo "dev-env: another checkout may be up, or this one already is." >&2
		echo "dev-env: override with e.g. WORKTREE_INDEX=$((index + 1)) just up" >&2
		exit 1
	fi
	exit 0
fi

printf 'export WORKTREE_INDEX=%q\n' "$index"
# For kind clusters, ctlptl's Cluster object name is the kubectl context name.
printf 'export CLUSTER_NAME=%q\n' kind-kind
printf 'export K8S_CONTEXT=%q\n' kind-kind
printf 'export K8S_NAMESPACE=%q\n' "${K8S_NAMESPACE:-$default_namespace}"
printf 'export TILT_PORT=%q\n' "$TILT_PORT"
printf 'export WEB_PORT=%q\n' "$WEB_PORT"
printf 'export DB_PORT=%q\n' "$DB_PORT"
printf 'export S3_PORT=%q\n' "$S3_PORT"
printf 'export S3_CONSOLE_PORT=%q\n' "$S3_CONSOLE_PORT"
printf 'export FILES_PORT=%q\n' "$FILES_PORT"
printf 'export SEARCH_PORT=%q\n' "$SEARCH_PORT"
