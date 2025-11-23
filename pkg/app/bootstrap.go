package app

import (
	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"
)

func BootstrapApp() *config.Config {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal("配置加载失败", "err", err)
	}

	// 初始化文件日志（根据配置）
	if err := logger.InitLogger(&cfg.Logger); err != nil {
		logger.Fatal("初始化 Logger 失败", "err", err)
	}
	logger.Info("Application bootstrapped successfully")

	return cfg
}
