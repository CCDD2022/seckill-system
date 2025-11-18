package mysql

import (
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	db *gorm.DB
)

func InitDB(cfg *config.MySQLConfig) (*gorm.DB, error) {
	// 构造 DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)
	// 使用 SkipDefaultTransaction 减少每次写入的事务开销；PrepareStmt 复用语句
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	})
	if err != nil {
		return nil, fmt.Errorf("数据库连接失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取原生DB失败: %v", err)
	}
	// 调整连接池：适度增加空闲数，设定生命周期防止长连接阻塞复用
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns * 2)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	db.AutoMigrate(
		&model.User{},
		&model.Product{},
		&model.Order{},
	)
	return db, nil

	
}

func GetDB() *gorm.DB {
	return db
}
