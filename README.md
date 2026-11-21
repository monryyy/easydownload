# EasyDownload

A small desktop download manager written in Go with a vanilla JavaScript UI,
built with [Wails v2](https://wails.io/).

This test build supports HTTP and HTTPS downloads on Linux, Windows and macOS.

## Features

- Parallel segmented downloads (up to 8 connections per download)
- Pause / resume across restarts (JSON state files + `.part` files)
- Automatic retries with exponential backoff
- Optional SHA-256 / SHA-1 / MD5 verification
- Global speed limiter
- Queue with configurable concurrency
- Native folder picker, `xdg-open` integration
- Dark desktop UI with live progress, speed and ETA

## Requirements

- Go 1.22+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- GTK3 and WebKit2GTK development files

Arch Linux:

```bash
sudo pacman -S gtk3 webkit2gtk-4.1
```

Debian / Ubuntu:

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev build-essential pkg-config
```

On systems with WebKit2GTK 4.1, the `webkit2_41` build tag is required. On
systems with 4.0, remove the tag from the Makefile.

## Run

```bash
make dev      # live-reloading development window
make build    # produces build/bin/EasyDownload
make test     # engine integration tests
make test-race
```

## Launching without `make`

Once built, you can launch the app directly. The shortcuts below point at
`build/bin/EasyDownload`, so a later `make build` updates what launches.

**Linux**

- Add a desktop entry to launch it from your application menu:

  ```bash
  mkdir -p ~/.local/share/applications
  cat > ~/.local/share/applications/easydownload.desktop <<EOF
  [Desktop Entry]
  Type=Application
  Name=EasyDownload
  Exec=$PWD/build/bin/EasyDownload
  Icon=$PWD/build/appicon.png
  Terminal=false
  Categories=Network;FileTransfer;
  EOF
  update-desktop-database ~/.local/share/applications
  ```

- Or add a shell shortcut so you can just type `easydownload`:

  ```bash
  ln -s "$PWD/build/bin/EasyDownload" ~/.local/bin/easydownload
  ```

  (`~/.local/bin` must be on your `PATH`.)

**Windows**

Extract `EasyDownload-*-windows-amd64.zip` and right-click `EasyDownload.exe`
→ *Pin to Start* or *Pin to taskbar*. Windows 10/11 includes the WebView2
runtime; older systems need the [WebView2 runtime](https://developer.microsoft.com/microsoft-edge/webview2/)
installed.

**macOS**

Extract `EasyDownload-*-macos-universal.zip` and drag `EasyDownload.app` into
`/Applications`. If macOS blocks it, run `xattr -cr /Applications/EasyDownload.app`
once (the binaries are unsigned).

## Portable builds

```bash
make release
```

This writes `dist/` archives for the host OS. On Linux it produces both
`EasyDownload-*-linux-amd64.tar.gz` and `EasyDownload-*-windows-amd64.zip`
(Windows cross-compiles cleanly because Wails uses the pure-Go WebView2
loader). `SHA256SUMS` is generated alongside them.

macOS cannot be cross-compiled — Wails requires native CGO frameworks. Build
it on a Mac:

```bash
make release     # on macOS: dist/EasyDownload-*-macos-universal.zip
```

Alternatively, `.github/workflows/release.yml` builds Linux, Windows and
macOS on native GitHub runners. Push a `v*` tag (or trigger the workflow
manually) to get all three archives as artifacts and, for tags, as a GitHub
release.

## Where files go

- Settings: `~/.config/easydownload/config.json` (or `$XDG_CONFIG_HOME`)
- Download state: `~/.local/share/easydownload/<uuid>.json`
- Downloads: `~/Downloads` by default (change in Settings)
- Partial files: `<filename>.part`

## Architecture

```
app.go              Wails bindings exposed to the UI
main.go             app bootstrap
internal/model      shared data types
internal/config     JSON config load/save
internal/store      JSON state persistence
internal/engine     queue, HTTP workers, resume, retry, verification
frontend/           vanilla JS/CSS UI
```

The Go engine emits `download:state`, `download:progress`, `download:complete`
and `download:error` events to the frontend over the Wails event bus.

## Not included in this test build

BitTorrent/magnet, FTP/SFTP, mirrors, per-download speed limits, scheduling,
browser integration.
