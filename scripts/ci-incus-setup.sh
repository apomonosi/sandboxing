#!/usr/bin/env bash
# Best-effort helper to install and initialize Incus for the gated
# integration test suite (internal/provider/incus/integration_test.go).
# Only invoked from .github/workflows/ci-integration.yml, never from the
# default unit-test CI job.
set -euo pipefail

sudo apt-get update
sudo apt-get install -y incus
sudo incus admin init --minimal
sudo usermod -aG incus-admin "$USER"
