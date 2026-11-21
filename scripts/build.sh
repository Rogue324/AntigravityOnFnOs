#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/frontend"
BACKEND_DIR="${ROOT_DIR}/backend"
FPK_TEMPLATE_DIR="${ROOT_DIR}/fpk"
DIST_DIR="${ROOT_DIR}/dist"
FNPACK_VERSION="${FNPACK_VERSION:-1.2.3}"

ARCH_LIST=("${@:-amd64}")
if [ "${1:-}" = "all" ]; then
  ARCH_LIST=("amd64" "arm64")
fi

ensure_fnpack() {
  if command -v fnpack >/dev/null 2>&1; then
    command -v fnpack
    return 0
  fi

  local tools_dir="${DIST_DIR}/.tools"
  local host_arch
  case "$(uname -m)" in
    x86_64|amd64) host_arch="amd64" ;;
    aarch64|arm64) host_arch="arm64" ;;
    *)
      echo "Unsupported host arch for fnpack: $(uname -m)" >&2
      return 1
      ;;
  esac

  local bin_path="${tools_dir}/fnpack-${FNPACK_VERSION}-linux-${host_arch}"
  if [ ! -x "${bin_path}" ]; then
    mkdir -p "${tools_dir}"
    local url="https://static2.fnnas.com/fnpack/fnpack-${FNPACK_VERSION}-linux-${host_arch}"
    echo "==> Downloading fnpack from ${url}..." >&2
    curl -fsSL "${url}" -o "${bin_path}"
    chmod +x "${bin_path}"
  fi
  echo "${bin_path}"
}

echo "==> [1/3] Building frontend SPA..."
cd "${FRONTEND_DIR}"
npm ci --no-audit --no-fund || npm install --no-audit --no-fund
npm run build

mkdir -p "${DIST_DIR}"
FNPACK_BIN="$(ensure_fnpack)"

for GOARCH in "${ARCH_LIST[@]}"; do
  case "${GOARCH}" in
    amd64|x86|x86_64)
      GOARCH="amd64"
      FNOS_PLATFORM="x86"
      ;;
    arm64|arm|aarch64)
      GOARCH="arm64"
      FNOS_PLATFORM="arm"
      ;;
    *)
      echo "Unsupported arch: ${GOARCH} (use amd64, arm64, or all)" >&2
      exit 1
      ;;
  esac

  echo "==> [2/3] Cross-compiling Go backend (linux/${GOARCH})..."
  STAGE_DIR="${DIST_DIR}/stage-${FNOS_PLATFORM}"
  rm -rf "${STAGE_DIR}"
  cp -a "${FPK_TEMPLATE_DIR}" "${STAGE_DIR}"
  mkdir -p "${STAGE_DIR}/app/server"
  rm -f "${STAGE_DIR}/wizard/.gitkeep"

  (
    cd "${BACKEND_DIR}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
      go build -trimpath -ldflags="-s -w" \
      -o "${STAGE_DIR}/app/server/agy-fnos" .
  )

  chmod +x "${STAGE_DIR}/app/server/agy-fnos" "${STAGE_DIR}/cmd/"*
  sed -i "s/^platform=.*/platform=${FNOS_PLATFORM}/" "${STAGE_DIR}/manifest"

  echo "==> [3/3] Packaging fnOS FPK (${GOARCH} / platform=${FNOS_PLATFORM})..."
  OUT_FPK="${DIST_DIR}/antigravity-cli-${GOARCH}.fpk"
  rm -f "${OUT_FPK}" "${OUT_FPK}.sha256" "${DIST_DIR}/antigravity-cli.fpk"
  (
    cd "${DIST_DIR}"
    "${FNPACK_BIN}" build --directory "${STAGE_DIR}"
    mv "${DIST_DIR}/antigravity-cli.fpk" "${OUT_FPK}"
    sha256sum "$(basename "${OUT_FPK}")" > "${OUT_FPK}.sha256"
  )
  echo "    Built: ${OUT_FPK} ($(stat -c%s "${OUT_FPK}") bytes)"
  cat "${OUT_FPK}.sha256"
done

echo "==> Done! Output in ${DIST_DIR}"
