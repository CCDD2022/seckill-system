package logger

// 恢复文件日志能力：使用 slog + lumberjack，实现按配置输出到文件或 stdout。
// 保留原有简单调用接口 (Info/Warn/Error 等)，减少侵入性修改。

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/CCDD2022/seckill-system/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

var base *slog.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

// InitLogger 根据配置初始化全局 logger。
func InitLogger(cfg *config.Logger) error {
	if cfg == nil {
		return errors.New("nil logger config")
	}

	// 解析级别
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// 输出 writer
	var writer io.Writer = os.Stdout
	if cfg.Output == "file" {
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		writer = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   true,
		}
	}

	opts := &slog.HandlerOptions{Level: level, AddSource: cfg.Level == "debug"}
	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	default: // text 或未知
		handler = slog.NewTextHandler(writer, opts)
	}

	base = slog.New(handler)
	base.Info("logger initialized", "level", cfg.Level, "format", cfg.Format, "output", cfg.Output, "file", cfg.FilePath)
	return nil
}

// Debug 输出调试日志。
func Debug(msg string, args ...any) { base.Debug(msg, args...) }

// Info 输出普通信息日志。
func Info(msg string, args ...any) { base.Info(msg, args...) }

// Warn 输出警告日志。
func Warn(msg string, args ...any) { base.Warn(msg, args...) }

// Error 输出错误日志。
func Error(msg string, args ...any) { base.Error(msg, args...) }

// DebugContext 携带 context 的调试日志。
func DebugContext(ctx context.Context, msg string, args ...any) { base.DebugContext(ctx, msg, args...) }

// InfoContext 携带 context 的信息日志。
func InfoContext(ctx context.Context, msg string, args ...any) { base.InfoContext(ctx, msg, args...) }

// WarnContext 携带 context 的警告日志。
func WarnContext(ctx context.Context, msg string, args ...any) { base.WarnContext(ctx, msg, args...) }

// ErrorContext 携带 context 的错误日志。
func ErrorContext(ctx context.Context, msg string, args ...any) { base.ErrorContext(ctx, msg, args...) }

// With 生成带额外字段的新 logger。
func With(args ...any) *slog.Logger { return base.With(args...) }

// Fatal 记录错误并退出进程（状态码1）。
func Fatal(msg string, args ...any) {
	base.Error(msg, args...)
	os.Exit(1)
}
