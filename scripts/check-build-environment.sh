#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[ "$#" -eq 0 ] || { echo 'BUILD_DECLARATION_INVALID: usage: check-build-environment.sh' >&2; exit 78; }
node_expected=$(awk 'NF { value=$0; count++ } END { if (count == 1) print value; else exit 1 }' "$root/.node-version" 2>/dev/null || true)
node_declared=$(node -e 'const v=require(process.argv[1]);process.stdout.write(v.engines?.node??"")' "$root/package.json" 2>/dev/null || true)
package_manager=$(node -e 'const v=require(process.argv[1]);process.stdout.write(v.packageManager??"")' "$root/package.json" 2>/dev/null || true)
case "$package_manager" in pnpm@*) pnpm_expected=${package_manager#pnpm@} ;; *) pnpm_expected= ;; esac
go_expected=$(awk '$1 == "go" { value="go" $2; count++ } END { if (count == 1) print value; else exit 1 }' "$root/go.mod" 2>/dev/null || true)
[ -n "$node_expected" ] && [ "$node_expected" = "$node_declared" ] && [ -n "$pnpm_expected" ] && [ -n "$go_expected" ] || { echo 'BUILD_DECLARATION_INVALID: Node, pnpm and Go owners must be exact' >&2; exit 78; }

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) platform=darwin; node_arch=arm64; go_arch=arm64 ;;
  Darwin-x86_64) if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then platform=darwin; node_arch=arm64; go_arch=arm64; else platform=darwin; node_arch=x64; go_arch=amd64; fi ;;
  Linux-aarch64|Linux-arm64) platform=linux; node_arch=arm64; go_arch=arm64 ;;
  Linux-x86_64) platform=linux; node_arch=x64; go_arch=amd64 ;;
  MINGW*-x86_64|MSYS*-x86_64|CYGWIN*-x86_64) platform=windows; node_arch=x64; go_arch=amd64 ;;
  *) echo 'TOOLCHAIN_MISMATCH: unsupported host' >&2; exit 78 ;;
esac
node_platform=$platform
[ "$platform" != windows ] || node_platform=win32
node_actual=$(node --version 2>/dev/null || true)
node_actual_platform=$(node -p process.platform 2>/dev/null || true)
node_actual_arch=$(node -p process.arch 2>/dev/null || true)
pnpm_actual=$(pnpm --version 2>/dev/null || true)
pnpm_command=$(command -v pnpm 2>/dev/null || true)
pnpm_executable=$(node -e 'const f=require("fs"),p=require("path");let d;try{d=p.dirname(f.realpathSync(process.argv[1]))}catch{process.exit(2)}for(;;){const m=p.join(d,"package.json");if(f.existsSync(m)){try{const v=JSON.parse(f.readFileSync(m));if(v.name==="pnpm"){process.stdout.write(v.version);break}}catch{}}const q=p.dirname(d);if(q===d)process.exit(2);d=q}' "$pnpm_command" 2>/dev/null || true)
go_actual=$(go env GOVERSION 2>/dev/null || true)
go_os=$(go env GOHOSTOS 2>/dev/null || true)
go_actual_arch=$(go env GOHOSTARCH 2>/dev/null || true)
if [ "$node_actual" != "v$node_expected" ] || [ "$node_actual_platform" != "$node_platform" ] || [ "$node_actual_arch" != "$node_arch" ] || \
   [ "$pnpm_actual" != "$pnpm_expected" ] || [ "$pnpm_executable" != "$pnpm_expected" ] || \
   [ "$go_actual" != "$go_expected" ] || [ "$go_os" != "$platform" ] || [ "$go_actual_arch" != "$go_arch" ]; then
  printf 'TOOLCHAIN_MISMATCH: expected node=v%s pnpm=%s go=%s runtime=%s nodeArch=%s goArch=%s; actual node=%s pnpm=%s pnpmExecutable=%s go=%s nodeRuntime=%s/%s goRuntime=%s/%s\n' \
    "$node_expected" "$pnpm_expected" "$go_expected" "$platform" "$node_arch" "$go_arch" \
    "${node_actual:-missing}" "${pnpm_actual:-missing}" "${pnpm_executable:-unknown}" "${go_actual:-missing}" \
    "${node_actual_platform:-unknown}" "${node_actual_arch:-unknown}" "${go_os:-unknown}" "${go_actual_arch:-unknown}" >&2
  exit 78
fi
lock=$(shasum -a 256 "$root/pnpm-lock.yaml" | awk '{print $1}')
printf 'BUILD_ENVIRONMENT_READY node=%s pnpm=%s go=%s runtime=%s nodeArch=%s goArch=%s lockSHA256=%s\n' \
  "$node_actual" "$pnpm_actual" "$go_actual" "$platform" "$node_arch" "$go_arch" "$lock"
