// Command ohmysmsd 是 ohmysmsapp 的后端守护进程。
//
// 阶段 1（当前）：加载配置 → 打开 SQLite → 启动 HTTP 服务（含 SPA 托管）→ 优雅退出。
// 后续阶段会在此注入 ModemManager、Telegram、ESIM 等子系统。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kimmy/ohmysmsapp/backend/internal/config"
	"github.com/kimmy/ohmysmsapp/backend/internal/db"
	"github.com/kimmy/ohmysmsapp/backend/internal/httpapi"
	"github.com/kimmy/ohmysmsapp/backend/internal/logging"
)

// 编译期注入。Makefile 会通过 -ldflags 替换。
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Printf("ohmysmsd %s (%s)\n", version, commit)
		return nil
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	log.Info("starting ohmysmsd",
		"version", version, "commit", commit,
		"listen", cfg.Server.Listen, "db", cfg.Database.Path)

	rootCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	conn, err := db.Open(rootCtx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()
	log.Info("database ready")

	// TODO(stage-2): 启动 ModemManager client 并订阅
	// TODO(stage-3): 启动 WS hub
	// TODO(stage-5): 启动 Telegram bot
	// TODO(stage-6): 初始化 ESIM runner

	handler := httpapi.NewRouter(httpapi.Deps{
		Version: version,
		WebRoot: cfg.Server.WebRoot,
	})
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.Server.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("bye")
	return nil
}
