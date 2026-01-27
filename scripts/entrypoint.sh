#!/usr/bin/env bash
set -euo pipefail

run_archiver() {
  mwarchiver "$@"
}

create_release() {
  local tag="$1"
  local repo="$2"
  local token="$3"

  local release_json
  release_json="$(curl -sS -H "Authorization: Bearer ${token}" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${repo}/releases/tags/${tag}" || true)"

  if echo "${release_json}" | jq -e '.id' >/dev/null 2>&1; then
    echo "${release_json}"
    return 0
  fi

  curl -sS -H "Authorization: Bearer ${token}" \
    -H "Accept: application/vnd.github+json" \
    -d "{\"tag_name\":\"${tag}\",\"name\":\"${tag}\",\"prerelease\":false}" \
    "https://api.github.com/repos/${repo}/releases"
}

delete_asset_if_exists() {
  local release_id="$1"
  local asset_name="$2"
  local repo="$3"
  local token="$4"

  local assets_json
  assets_json="$(curl -sS -H "Authorization: Bearer ${token}" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${repo}/releases/${release_id}/assets")"

  local asset_id
  asset_id="$(echo "${assets_json}" | jq -r ".[] | select(.name==\"${asset_name}\") | .id" | head -n 1)"

  if [[ -n "${asset_id}" && "${asset_id}" != "null" ]]; then
    curl -sS -X DELETE -H "Authorization: Bearer ${token}" \
      -H "Accept: application/vnd.github+json" \
      "https://api.github.com/repos/${repo}/releases/assets/${asset_id}" >/dev/null
  fi
}

upload_asset() {
  local upload_url="$1"
  local zip_path="$2"
  local token="$3"
  local asset_name
  asset_name="$(basename "${zip_path}")"

  local upload_endpoint
  upload_endpoint="${upload_url%\{*}?name=${asset_name}"

  curl -sS -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/zip" \
    --data-binary @"${zip_path}" \
    "${upload_endpoint}" >/dev/null
}

main() {
  run_archiver "$@"

  if [[ "${RELEASE_UPLOAD:-}" != "" ]]; then
    local repo="${GITHUB_REPOSITORY:-}"
    local token="${GITHUB_TOKEN:-}"
    if [[ -z "${repo}" || -z "${token}" ]]; then
      echo "RELEASE_UPLOAD set but GITHUB_REPOSITORY or GITHUB_TOKEN missing" >&2
      exit 1
    fi

    local release_tag="${RELEASE_TAG:-}"
    if [[ -z "${release_tag}" ]]; then
      release_tag="$(date -u +%Y.%m.%d)"
    fi

    local db_path="${DB_PATH:-mwarchiver.db}"
    local zip_name="${RELEASE_ASSET_NAME:-mwarchiver-${release_tag}.zip}"

    if [[ ! -f "${db_path}" ]]; then
      echo "Database not found at ${db_path}" >&2
      exit 1
    fi

    zip -j "${zip_name}" "${db_path}" >/dev/null

    local release_json
    release_json="$(create_release "${release_tag}" "${repo}" "${token}")"
    local release_id
    release_id="$(echo "${release_json}" | jq -r '.id')"
    local upload_url
    upload_url="$(echo "${release_json}" | jq -r '.upload_url')"

    if [[ "${RELEASE_OVERWRITE:-}" != "" ]]; then
      delete_asset_if_exists "${release_id}" "${zip_name}" "${repo}" "${token}"
    fi

    upload_asset "${upload_url}" "${zip_name}" "${token}"
  fi
}

main "$@"
