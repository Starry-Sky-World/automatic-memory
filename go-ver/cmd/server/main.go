package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"deepseek2api-go/internal/accounts"
	"deepseek2api-go/internal/clients"
	"deepseek2api-go/internal/cloudsync"
	"deepseek2api-go/internal/config"
	"deepseek2api-go/internal/httpserver"
	"deepseek2api-go/internal/logging"
	"deepseek2api-go/internal/pow"
	"deepseek2api-go/internal/state"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)
	httpClient := clients.NewHTTPClient(cfg)
	pool := accounts.NewPool(cfg, httpClient)
	solver := pow.NewSolver()
	cache := pow.NewCache()
	if err := solver.Warmup(); err != nil {
		logger.Warnf("PoW solver warmup failed: %v", err)
	}
	ds := clients.NewDeepSeekClient(httpClient, cfg.URLSession(), cfg.URLCreatePow(), cfg.URLCompletion())
	st := state.NewAppState(cfg, logger, httpClient, pool, solver, cache, ds)

	if cfg.CloudSync.Enabled {
		if cfg.CloudSync.BaseURL == "" {
			logger.Warnf("cloudsync enabled but base_url is empty")
			st.MarkSyncError("cloudsync base_url is empty")
		} else {
			csClient := cloudsync.NewClient(httpClient, cfg.CloudSync.BaseURL, cfg.CloudSync.Token, cfg.CloudSync.UserID)
			sm := cloudsync.NewSyncManager(st, csClient, cfg.CloudSync)
			st.Sync = sm
			if err := sm.InitialSync(context.Background()); err != nil {
				logger.Warnf("cloudsync initial sync failed: %v", err)
			}
		}
	}

	router := httpserver.NewRouter(st)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 10 * time.Second}

	syncCtx, syncCancel := context.WithCancel(context.Background())
	if sm, ok := st.Sync.(*cloudsync.SyncManager); ok {
		go sm.Run(syncCtx)
	}

	go func() {
		logger.Infof("server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("server error: %v", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	syncCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
