// Package logging 提供 slog 的默认配置。
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New 按照 level/format 构造 slog.Logger。
// level 支持 debug/info/warn/error；format 支持 text/json。
func New(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	var w io.Writer = os.Stdout
	switch strings.ToLower(format) {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}
