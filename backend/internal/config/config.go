// Package config 加载并校验 ohmysmsd 的配置。
//
// 配置优先级：命令行 > 环境变量 (OHMYSMS_*) > 配置文件 > 默认值。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Modem    ModemConfig    `yaml:"modem"`
	Telegram TelegramConfig `yaml:"telegram"`
	ESIM     ESIMConfig     `yaml:"esim"`
	Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
	Listen         string   `yaml:"listen"`
	WebRoot        string   `yaml:"web_root"`
	BasePath       string   `yaml:"base_path"`
	AllowedOrigins []string `yaml:"allowed_origins"` // CORS 白名单；空=同源
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type AuthConfig struct {
	JWTSecret      string        `yaml:"jwt_secret"`
	Username       string        `yaml:"username"`
	PasswordBcrypt string        `yaml:"password_bcrypt"`
	TokenTTL       time.Duration `yaml:"token_ttl"`
}

type ModemConfig struct {
	Enabled    bool          `yaml:"enabled"`
	SignalPoll time.Duration `yaml:"signal_poll"`
}

type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   int64  `yaml:"chat_id"`
	PushSMS  bool   `yaml:"push_sms"`
}

type ESIMConfig struct {
	LPACBin             string            `yaml:"lpac_bin"`
	LPACDriversDir      string            `yaml:"lpac_drivers_dir"`
	InhibitModemManager bool              `yaml:"inhibit_modem_manager"`
	AIDs                map[string]string `yaml:"aids"`
	OperationTimeout    time.Duration     `yaml:"operation_timeout"` // lpac 单次执行超时
	DiscoverCooldown    time.Duration     `yaml:"discover_cooldown"` // 同一 modem 自动发现冷却
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default 返回带有默认值的配置。
func Default() Config {
	return Config{
		Server: ServerConfig{
			Listen:   "0.0.0.0:8080",
			WebRoot:  "./web",
			BasePath: "/",
		},
		Database: DatabaseConfig{
			Path: "./data/ohmysmsapp.db",
		},
		Auth: AuthConfig{
			Username: "admin",
			TokenTTL: 30 * 24 * time.Hour,
		},
		Modem: ModemConfig{
			Enabled:    true,
			SignalPoll: 30 * time.Second,
		},
		Telegram: TelegramConfig{
			PushSMS: true,
		},
		ESIM: ESIMConfig{
			LPACBin:             "/opt/ohmysmsapp/bin/lpac",
			LPACDriversDir:      "/opt/ohmysmsapp/bin/lpac-drivers",
			InhibitModemManager: true,
			AIDs:                map[string]string{},
			OperationTimeout:    30 * time.Second,
			DiscoverCooldown:    1 * time.Hour,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Load 从 path 读取 yaml 并与默认值合并；path 为空时返回默认配置。
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cfg, fmt.Errorf("resolve config path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", abs, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	return cfg, cfg.Validate()
}

// Validate 检查必填字段和显然错误的值。
func (c Config) Validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path is required")
	}
	if c.Auth.TokenTTL <= 0 {
		return fmt.Errorf("auth.token_ttl must be positive")
	}
	if c.Modem.SignalPoll < time.Second {
		return fmt.Errorf("modem.signal_poll must be >= 1s")
	}
	return nil
}
