package app

import (
	"log"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"
)

func BootstrapApp() *config.Config {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	// 初始化 Logger
	if err := logger.InitLogger(&cfg.Logger); err != nil {
		log.Fatalf("初始化 Logger 失败: %v", err)
	}

	logger.Info("Application bootstrapped successfully")

	return cfg
}
