#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="${ROOT_DIR}/frontend"
BACKEND_DIR="${ROOT_DIR}/backend"
FPK_TEMPLATE_DIR="${ROOT_DIR}/fpk"
DIST_DIR="${ROOT_DIR}/dist"

ARCH_LIST=("${@:-amd64}")
if [ "${1:-}" = "all" ]; then
  ARCH_LIST=("amd64" "arm64")
fi

echo "==> [1/3] Building frontend SPA..."
cd "${FRONTEND_DIR}"
npm ci --no-audit --no-fund || npm install --no-audit --no-fund
npm run build

mkdir -p "${DIST_DIR}"

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

  (
    cd "${BACKEND_DIR}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
      go build -trimpath -ldflags="-s -w" \
      -o "${STAGE_DIR}/app/server/agy-fnos" .
  )

  chmod +x "${STAGE_DIR}/app/server/agy-fnos" "${STAGE_DIR}/cmd/"*
  sed -i "s/^platform=.*/platform=${FNOS_PLATFORM}/" "${STAGE_DIR}/manifest"

  echo "==> [3/3] Packaging fnOS FPK (${FNOS_PLATFORM})..."
  if command -v fnpack >/dev/null 2>&1; then
    (
      cd "${DIST_DIR}"
      fnpack build --directory "${STAGE_DIR}"
      if [ -f "${DIST_DIR}/antigravity-cli.fpk" ]; then
        mv "${DIST_DIR}/antigravity-cli.fpk" "${DIST_DIR}/antigravity-cli-${FNOS_PLATFORM}.fpk"
      fi
    )
    echo "    Built: ${DIST_DIR}/antigravity-cli-${FNOS_PLATFORM}.fpk"
  else
    echo "    [warn] 'fnpack' CLI not found in PATH."
    echo "    Prepared FPK directory at: ${STAGE_DIR}"
    echo "    Run 'fnpack build --directory ${STAGE_DIR}' once fnpack is installed."
  fi
done

echo "==> Done! Output in ${DIST_DIR}"
