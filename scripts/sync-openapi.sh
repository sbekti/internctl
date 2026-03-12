#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "${script_dir}/.." && pwd)
source_spec="${repo_root}/../intern-api/api/openapi.yaml"
target_spec="${repo_root}/api/openapi.yaml"

if [[ ! -f "${source_spec}" ]]; then
  echo "missing source spec: ${source_spec}" >&2
  exit 1
fi

cp "${source_spec}" "${target_spec}"
echo "synced ${target_spec}"
