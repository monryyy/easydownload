package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"EasyDownload/internal/config"
	"EasyDownload/internal/engine"
	"EasyDownload/internal/store"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App exposes the engine to the web frontend.
type App struct {
	ctx    context.Context
	engine *engine.Engine
	cfg    *config.Config
	err    error
}

// NewApp creates the App instance.
func NewApp() *App {
	return &App{}
}

// startup wires configuration, storage, engine and the event bridge.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := config.Load()
	if err != nil {
		a.err = fmt.Errorf("could not load settings: %w", err)
		return
	}
	a.cfg = cfg

	st, err := store.New(cfg.StateDir)
	if err != nil {
		a.err = fmt.Errorf("could not open state directory: %w", err)
		return
	}

	a.engine, err = engine.New(cfg, st, func(name string, data interface{}) {
		wailsRuntime.EventsEmit(ctx, name, data)
	})
	if err != nil {
		a.err = fmt.Errorf("could not start engine: %w", err)
	}
}

func (a *App) ready() error {
	if a.err != nil {
		return a.err
	}
	if a.engine == nil {
		return errors.New("engine is not ready")
	}
	return nil
}

func normalizeChecksum(algo, hex string) *engine.Checksum {
	if hex == "" {
		return nil
	}
	return &engine.Checksum{Algo: algo, Hex: hex}
}

// AddDownload queues a new URL. Pass empty strings for no checksum.
func (a *App) AddDownload(url string, checksumAlgo string, checksumHex string) (engine.Download, error) {
	if err := a.ready(); err != nil {
		return engine.Download{}, err
	}
	return a.engine.Add(url, normalizeChecksum(checksumAlgo, checksumHex))
}

// ListDownloads returns all downloads, newest first.
func (a *App) ListDownloads() ([]engine.Download, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.engine.List(), nil
}

// PauseDownload stops a download without deleting partial data.
func (a *App) PauseDownload(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.engine.Pause(id)
}

// ResumeDownload continues a paused or failed download.
func (a *App) ResumeDownload(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.engine.Resume(id)
}

// CancelDownload stops a download and deletes its partial file.
func (a *App) CancelDownload(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.engine.Cancel(id)
}

// RemoveDownload deletes a download record and optionally its finished file.
func (a *App) RemoveDownload(id string, deleteFile bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.engine.Remove(id, deleteFile)
}

// GetConfig returns the current settings.
func (a *App) GetConfig() (config.Config, error) {
	if err := a.ready(); err != nil {
		return config.Config{}, err
	}
	return a.engine.Config(), nil
}

// SetConfig saves new settings and applies them live.
func (a *App) SetConfig(cfg config.Config) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.engine.SetConfig(&cfg); err != nil {
		return err
	}
	a.cfg = &cfg
	return nil
}

// PickDownloadDir opens a native folder chooser.
func (a *App) PickDownloadDir() (string, error) {
	if a.ctx == nil {
		return "", errors.New("app context is not ready")
	}
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Choose download folder",
	})
}

// OpenPath opens a file or folder with the desktop's default application.
func (a *App) OpenPath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	cmd := exec.Command("xdg-open", path)
	return cmd.Start()
}

// RevealFile opens the folder containing a file.
func (a *App) RevealFile(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	return a.OpenPath(filepath.Dir(path))
}
