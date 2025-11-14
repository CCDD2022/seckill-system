package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/CCDD2022/seckill-system/config" // 根据你的实际模块名调整

	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 包装 slog.Logger，提供便捷方法
type Logger struct {
	*slog.Logger
}

var logger *Logger

// InitLogger 初始化全局日志实例
func InitLogger(cfg *config.Logger) error {
	var (
		handler slog.Handler
		level   slog.Level
		writer  io.Writer
	)

	// 1. 解析日志级别
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

	// 2. 设置输出目标
	switch cfg.Output {
	case "file":
		// 确保日志目录存在
		logDir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}

		writer = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   true, // 压缩旧日志
		}
	case "stdout":
		writer = os.Stdout
	default:
		writer = os.Stdout
	}

	// 3. 创建 Handler
	opts := &slog.HandlerOptions{
		Level: level,
		// 添加 source 信息（源文件和行号）
		AddSource: cfg.Level == "debug",
	}

	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "text":
		handler = slog.NewTextHandler(writer, opts)
	default:
		handler = slog.NewJSONHandler(writer, opts)
	}

	// 4. 创建全局 logger
	logger = &Logger{
		Logger: slog.New(handler),
	}

	Info("Logger initialized successfully", "config", cfg)
	return nil
}

// GetLogger 获取全局 logger 实例
func GetLogger() *Logger {
	if logger == nil {
		return &Logger{Logger: slog.Default()}
	}
	return logger
}

// ========== 便捷方法 ==========

// Debug 记录 debug 日志
func Debug(msg string, args ...any) {
	GetLogger().Debug(msg, args...)
}

// Info 记录 info 日志
func Info(msg string, args ...any) {
	GetLogger().Info(msg, args...)
}

// Warn 记录 warn 日志
func Warn(msg string, args ...any) {
	GetLogger().Warn(msg, args...)
}

// Error 记录 error 日志
func Error(msg string, args ...any) {
	GetLogger().Error(msg, args...)
}

// DebugContext 带 context 的 debug 日志
func DebugContext(ctx context.Context, msg string, args ...any) {
	GetLogger().DebugContext(ctx, msg, args...)
}

// InfoContext 带 context 的 info 日志
func InfoContext(ctx context.Context, msg string, args ...any) {
	GetLogger().InfoContext(ctx, msg, args...)
}

// WarnContext 带 context 的 warn 日志
func WarnContext(ctx context.Context, msg string, args ...any) {
	GetLogger().WarnContext(ctx, msg, args...)
}

// ErrorContext 带 context 的 error 日志
func ErrorContext(ctx context.Context, msg string, args ...any) {
	GetLogger().ErrorContext(ctx, msg, args...)
}

// With 创建带自定义字段的 logger
func With(args ...any) *Logger {
	return &Logger{Logger: GetLogger().With(args...)}
}
