package mysql

import (
	"fmt"

	"github.com/CCDD2022/seckill-system/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	db *gorm.DB
)

func InitDB(cfg *config.MySQLConfig) (*gorm.DB, error) {

	// 1. 构造DSN数据源字符串
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)
	// 如果这里不声明err  那么下面:=会导致不赋值给全局变量db 而是另外创建一个局部变量db
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("数据库连接失败: %v", err)
	}

	// 获取原生sql.DB对象
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取原生DB失败: %v", err)
	}

	// 4. 设置连接池参数（从配置读取）
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns) // 最大连接数
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns) // 空闲连接数

	return db, nil
}

func GetDB() *gorm.DB {
	return db
}
