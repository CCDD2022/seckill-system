package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// ServicesConfig HTTP和gRPC服务器配置
type ServicesConfig struct {
	APIGateway     Service
	AuthService    Service
	UserService    Service
	ProductService Service
	SeckillService Service
	OrderService   Service
}

type Service struct {
	Host string
	Port int
}

// MySQLConfig 数据库配置
type MySQLConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DBName       string `yaml:"dbname"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// JWTConfig JWT认证配置
type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type Database struct {
	Mysql MySQLConfig `yaml:"mysql"`
	Redis RedisConfig `yaml:"redis"`
}

// Config 总配置结构体，嵌套所有子配置
type Config struct {
	Services ServicesConfig `yaml:"services"`
	Database Database       `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
}

var globalConfig *Config

func InitConfig(configPath string) error {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 读取内容
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败:%v", err)
	}
	globalConfig = &Config{}
	if err := viper.Unmarshal(globalConfig); err != nil {
		return fmt.Errorf("解析配置文件失败:%v", err)
	}

	return nil
}

func GetConfig() *Config {
	if globalConfig == nil {
		panic("配置未初始化，请先调用InitConfig")
	}
	return globalConfig
}
