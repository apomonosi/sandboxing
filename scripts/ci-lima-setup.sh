#!/usr/bin/env bash
# Best-effort helper to install Lima for the gated integration test suite
# (internal/provider/lima/integration_test.go). Only invoked from
# .github/workflows/ci-integration-lima.yml, never from the default
# unit-test CI job. Kept as its own script (even though it's currently a
# one-liner) for symmetry with scripts/ci-incus-setup.sh, and because it's
# the natural place to add `limactl --version` sanity-checking or
# template pre-warming later.
set -euo pipefail

brew install lima
limactl --version
