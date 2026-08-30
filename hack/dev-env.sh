#!/usr/bin/env bash
# dev-env.sh — print `export KEY=value` lines pinning this checkout's dev
# environment: the shared cluster's context, this checkout's namespace, and
# the per-checkout hostnames the shared gateway routes by. Sourced by the
# just recipes via `eval "$(hack/dev-env.sh)"`.
#
# All checkouts share ONE kind cluster ("kind", context "kind-kind") and one
# image registry, so a laptop can run many worktrees at once without paying
# for a control plane per checkout. Isolation is per-namespace, and local
# addressing is per-checkout HOSTNAME, not per-checkout port: a shared
# cloud-provider-kind gateway (installed by `just cluster-up`, see
# cluster/gateway.yaml) routes `<namespace>.<service>.localhost:$GATEWAY_PORT`
# to each checkout's services. GATEWAY_PORT is the ephemeral host port the
# gateway's envoy container publishes for listener :80; it is re-discovered
# on every run (override with GATEWAY_PORT=<n>).
#
#   main checkout  → namespace "default" → default.web.localhost, …
#   git worktree   → namespace "wt-<slug>-<hash>" → wt-….web.localhost, …
#
# The only remaining per-checkout host port is the Tilt UI (10350+N, with
# N = WORKTREE_INDEX, derived stably from the worktree path).
#
# Usage:
#   eval "$(hack/dev-env.sh)"   — export the environment
#   hack/dev-env.sh --check     — exit 1 if this checkout's Tilt UI port is
#                                 already listening (another checkout may be
#                                 up; override with WORKTREE_INDEX=<n>)
#
# Overrides: WORKTREE_INDEX=<n> forces the Tilt port index; K8S_NAMESPACE,
# TILT_PORT, DEV_TLD, GATEWAY_PORT, WEB_HOST, S3_HOST, S3_CONSOLE_HOST,
# S3_PUBLIC_ENDPOINT can each be set explicitly and are left untouched.

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

if [[ "${1:-}" == "--check" ]]; then
	# The preflight exists to stop concurrent local checkouts from fighting
	# over the one remaining per-checkout port (the Tilt UI). CI runners are
	# single-tenant, and an unrelated port being bound there (the GitHub
	# images ship postgres/mysql) must not fail a build, so skip it.
	if [[ -n "${CI:-}" ]]; then
		exit 0
	fi
	busy=()
	for port in "$TILT_PORT"; do
		if ss -tlnH "sport = :$port" | grep -q .; then
			busy+=("$port")
		fi
	done
	if ((${#busy[@]})); then
		echo "dev-env: Tilt UI port(s) already in use: ${busy[*]}" >&2
		echo "dev-env: only the Tilt UI port is per-checkout now; another" >&2
		echo "dev-env: checkout may be up, or this one already is." >&2
		echo "dev-env: override with e.g. WORKTREE_INDEX=$((index + 1)) just up" >&2
		exit 1
	fi
	exit 0
fi

ns="${K8S_NAMESPACE:-$default_namespace}"
DEV_TLD="${DEV_TLD:-localhost}"
if [[ -z "${GATEWAY_PORT:-}" ]]; then
	# The kind-gateway-proxy container (created by `just cluster-up`) forwards
	# 127.0.0.1:<ephemeral> to the shared gateway's envoy listener :80. It
	# binds IPv4 loopback ONLY: an IPv6 publish would accept-then-reset
	# (envoy binds IPv4-only inside its container), and *.localhost resolves
	# to ::1 first on systemd-resolved hosts, killing clients instead of
	# falling back to 127.0.0.1. Empty when the proxy isn't up yet — the
	# Tiltfile fails with a clear message in that case.
	GATEWAY_PORT="$({ docker port kind-gateway-proxy 80/tcp 2>/dev/null |
		sed -nE 's/.*127\.0\.0\.1:([0-9]+).*/\1/p' |
		head -1; } || true)"
fi

printf 'export WORKTREE_INDEX=%q\n' "$index"
# For kind clusters, ctlptl's Cluster object name is the kubectl context name.
printf 'export CLUSTER_NAME=%q\n' kind-kind
printf 'export K8S_CONTEXT=%q\n' kind-kind
printf 'export K8S_NAMESPACE=%q\n' "$ns"
printf 'export TILT_PORT=%q\n' "$TILT_PORT"
printf 'export DEV_TLD=%q\n' "$DEV_TLD"
printf 'export GATEWAY_PORT=%q\n' "$GATEWAY_PORT"
printf 'export WEB_HOST=%q\n' "${WEB_HOST:-$ns.web.$DEV_TLD}"
printf 'export S3_HOST=%q\n' "${S3_HOST:-$ns.s3.$DEV_TLD}"
printf 'export S3_CONSOLE_HOST=%q\n' "${S3_CONSOLE_HOST:-$ns.s3-console.$DEV_TLD}"
printf 'export S3_PUBLIC_ENDPOINT=%q\n' "${S3_PUBLIC_ENDPOINT:-$ns.s3.$DEV_TLD:$GATEWAY_PORT}"
