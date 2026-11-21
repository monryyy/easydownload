#!/usr/bin/env bash
# Builds portable binaries for the current host OS and writes them to dist/.
# Run on Linux to get Linux + Windows builds; on macOS for the macOS build.
set -euo pipefail

cd "$(dirname "$0")/.."

export PATH="$(go env GOPATH)/bin:$PATH"
command -v wails >/dev/null || { echo "wails CLI is required" >&2; exit 1; }

VERSION="${VERSION:-test}"
DIST="dist"
rm -rf "$DIST"
mkdir -p "$DIST"

zip_with_python() {
    python3 - "$1" "${@:2}" <<'PY'
import sys, zipfile
output, *paths = sys.argv[1:]
with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as z:
    for path in paths:
        import os
        if os.path.isdir(path):
            for root, _, files in os.walk(path):
                for name in files:
                    full = os.path.join(root, name)
                    z.write(full, os.path.relpath(full, os.path.dirname(path)))
        else:
            z.write(path, os.path.basename(path))
PY
}

os_name="$(uname -s)"

case "$os_name" in
    Linux*)
        LINUX_TAGS=""
        if pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
            LINUX_TAGS="webkit2_41"
        fi
        if [ -n "$LINUX_TAGS" ]; then
            wails build -clean -tags "$LINUX_TAGS" -platform linux/amd64 -o EasyDownload
        else
            wails build -clean -platform linux/amd64 -o EasyDownload
        fi
        tar -czf "$DIST/EasyDownload-${VERSION}-linux-amd64.tar.gz" -C build/bin EasyDownload

        wails build -clean -platform windows/amd64 -o EasyDownload.exe
        zip_with_python "$DIST/EasyDownload-${VERSION}-windows-amd64.zip" build/bin/EasyDownload.exe
        ;;

    Darwin*)
        wails build -clean -platform darwin/universal
        zip_with_python "$DIST/EasyDownload-${VERSION}-macos-universal.zip" build/bin/EasyDownload.app
        ;;

    MINGW*|MSYS*|CYGWIN*)
        wails build -clean -platform windows/amd64 -o EasyDownload.exe
        zip_with_python "$DIST/EasyDownload-${VERSION}-windows-amd64.zip" build/bin/EasyDownload.exe
        ;;

    *)
        echo "Unsupported host OS: $os_name" >&2
        exit 1
        ;;
esac

(cd "$DIST" && sha256sum * > SHA256SUMS)
echo "Release artifacts written to $DIST/"
ls -lh "$DIST"
