// Command ohmysmsd 是 ohmysmsapp 的后端守护进程。
//
// 阶段 3：完整的 HTTP REST API + WebSocket 实时推送。
// 子命令：
//   ohmysmsd -config config.yaml   正常启动
//   ohmysmsd hash-password <pw>    打印 bcrypt hash
//   ohmysmsd -version              打印版本
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KimmyXYC/ohmysmsapp/backend/internal/auth"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/config"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/db"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/httpapi"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/logging"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/modem"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/telegram"
	"github.com/KimmyXYC/ohmysmsapp/backend/internal/ws"
)

// 编译期注入。Makefile 会通过 -ldflags 替换。
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	// 子命令：hash-password
	if len(os.Args) >= 2 && os.Args[1] == "hash-password" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: ohmysmsd hash-password <password>")
			os.Exit(2)
		}
		h, err := auth.HashPassword(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "hash failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(h)
		return
	}

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

	// Auth service：jwt_secret 为空则运行期随机生成（仅内存），告警用户
	jwtSecret := cfg.Auth.JWTSecret
	if jwtSecret == "" {
		gen, err := auth.GenerateSecret()
		if err != nil {
			return fmt.Errorf("generate jwt secret: %w", err)
		}
		jwtSecret = gen
		log.Warn("auth.jwt_secret is empty; generated an ephemeral one — "+
			"tokens will invalidate on restart. set auth.jwt_secret in config.yaml for persistence",
			"hint_secret", gen[:8]+"...")
	}
	authSvc, err := auth.New(auth.Config{
		Secret:         []byte(jwtSecret),
		Username:       cfg.Auth.Username,
		PasswordBcrypt: cfg.Auth.PasswordBcrypt,
		TokenTTL:       cfg.Auth.TokenTTL,
	}, log)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}

	// Provider
	var provider modem.Provider
	if cfg.Modem.Enabled {
		provider = modem.NewMMProvider(log, 256)
		log.Info("modem provider: modemmanager")
	} else {
		provider = modem.NewMockProvider(log)
		log.Info("modem provider: mock")
	}
	modemStore := modem.NewStore(conn)
	runner := modem.NewRunner(provider, modemStore, log)

	runnerErrCh := make(chan error, 1)
	go func() {
		if err := runner.Run(rootCtx); err != nil {
			runnerErrCh <- err
		}
	}()

	// WebSocket hub
	hub := ws.NewHub(runner, authSvc, log, cfg.Server.AllowedOrigins)
	go hub.Run(rootCtx)

	// Telegram Bot
	tgCfg := loadTelegramConfig(rootCtx, cfg.Telegram, modemStore)
	tgCtl := telegram.NewController(provider, runner, modemStore, log)
	if err := tgCtl.Start(rootCtx, tgCfg); err != nil {
		log.Warn("telegram start failed", "err", err)
	}
	defer tgCtl.Stop()

	handler := httpapi.NewRouter(httpapi.Deps{
		Version:     version,
		WebRoot:     cfg.Server.WebRoot,
		Modem:       provider,
		ModemRunner: runner,
		Store:       modemStore,
		Auth:        authSvc,
		WSHandler:   hub,
		Server:      cfg.Server,
		Telegram:    cfg.Telegram,
		TelegramCtl: tgCtl,
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
	case err := <-runnerErrCh:
		return fmt.Errorf("modem runner: %w", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("bye")
	return nil
}

// loadTelegramConfig 合并 config.yaml 与 settings 表中的 telegram 配置。
// 策略：settings 表存在时整体覆盖 config.yaml（便于运行时 Web UI 改配置）。
func loadTelegramConfig(ctx context.Context, fileCfg config.TelegramConfig, store *modem.Store) config.TelegramConfig {
	raw, err := store.GetSetting(ctx, "telegram")
	if err != nil || raw == "" {
		return fileCfg
	}
	var saved config.TelegramConfig
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return fileCfg
	}
	return saved
}
