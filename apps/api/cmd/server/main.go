package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sudoriaa/atri-games/apps/api/internal/config"
	"github.com/sudoriaa/atri-games/apps/api/internal/data"
	"github.com/sudoriaa/atri-games/apps/api/internal/gamepkg"
	"github.com/sudoriaa/atri-games/apps/api/internal/httpapi"
	"github.com/sudoriaa/atri-games/apps/api/internal/objectstore"
	"github.com/sudoriaa/atri-games/apps/api/internal/security"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	store, err := data.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	adminHash, err := security.HashPassword(cfg.AdminPassword)
	if err != nil {
		logger.Error("hash seed password", "error", err)
		os.Exit(1)
	}
	if err := store.MigrateAndSeed(cfg.AdminEmail, adminHash); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}
	if err := store.RecoverGameImports(cfg.AssetRoot); err != nil {
		logger.Error("recover game imports", "error", err)
		os.Exit(1)
	}
	if err := store.RecoverManagedAssets(cfg.AssetRoot); err != nil {
		logger.Error("recover managed assets", "error", err)
		os.Exit(1)
	}
	if err := store.RecoverGameCoverUploads(cfg.AssetRoot); err != nil {
		logger.Error("recover game cover uploads", "error", err)
		os.Exit(1)
	}
	if err := backfillRuntimeBootstraps(store, cfg.AssetRoot, logger); err != nil {
		logger.Error("backfill game runtime bootstrap", "error", err)
		os.Exit(1)
	}
	assets, err := objectstore.New(cfg)
	if err != nil {
		logger.Error("configure object storage", "error", err)
		os.Exit(1)
	}
	if assets.Provider() != "local" {
		syncContext, cancelSync := context.WithTimeout(context.Background(), cfg.ObjectStorageSyncTimeout)
		err = assets.Ensure(syncContext)
		if err == nil {
			err = objectstore.SyncManagedAssetRoot(syncContext, assets, cfg.AssetRoot)
		}
		cancelSync()
		if err != nil {
			logger.Error("reconcile object storage", "provider", assets.Provider(), "error", err)
			os.Exit(1)
		}
		logger.Info("object storage ready", "provider", assets.Provider())
	}

	tokens := security.NewTokenManager(cfg.JWTSecret, 24*time.Hour)
	server := httpapi.NewWithObjectStore(cfg, store, tokens, logger, assets)

	shutdownSignals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api listening", "address", cfg.Address, "database", cfg.DatabasePath)
		if err := server.ListenAndServe(); err != nil {
			logger.Error("api stopped", "error", err)
			stop()
		}
	}()

	<-shutdownSignals.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}

// backfillRuntimeBootstraps upgrades entries already installed on the managed
// asset volume. Imports inject this bridge going forward; running it before
// the object-store reconciliation also mirrors repaired historical packages.
func backfillRuntimeBootstraps(store *data.Store, assetRoot string, logger *slog.Logger) error {
	entries, err := store.StaticPackageEntries()
	if err != nil {
		return err
	}
	for _, item := range entries {
		bundleRoot := filepath.Join(assetRoot, "playables", item.Slug)
		entryPath := filepath.Join(bundleRoot, filepath.FromSlash(item.Entry))
		relative, relErr := filepath.Rel(bundleRoot, entryPath)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			logger.Warn("skip unsafe static game entry during runtime backfill", "game", item.Slug)
			continue
		}
		changed, injectErr := gamepkg.InjectRuntimeBootstrap(entryPath)
		if injectErr != nil {
			logger.Warn("skip static game runtime backfill", "game", item.Slug, "error", injectErr)
			continue
		}
		if changed {
			logger.Info("backfilled static game runtime bootstrap", "game", item.Slug)
		}
	}
	return nil
}
