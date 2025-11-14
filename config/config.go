package config

import (
	"fmt"

	"github.com/spf13/viper"
)

//viper 内部使用 mapstructure 库进行配置解析，而不是直接使用 yaml 标签。
// Viper 默认情况下确实不能自动将连续的驼峰命名转换为蛇形命名。

// ServicesConfig HTTP和gRPC服务器配置
type ServicesConfig struct {
	APIGateway     Service `yaml:"api_gateway" mapstructure:"api_gateway"`
	UserService    Service `yaml:"user_service" mapstructure:"user_service"`
	ProductService Service `yaml:"product_service" mapstructure:"product_service"`
	SeckillService Service `yaml:"seckill_service" mapstructure:"seckill_service"`
	OrderService   Service `yaml:"order_service" mapstructure:"order_service"`
	AuthService    Service `yaml:"auth_service" mapstructure:"auth_service"`
}

type Service struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type Logger struct {
	Level      string
	Format     string
	Output     string
	FilePath   string `mapstructure:"file_path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
}

type ServerConfig struct {
	Port         int    `yaml:"port"`
	Mode         string `yaml:"mode"`
	readTimeout  int    `yaml:"read_timeout" mapstructure:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout" mapstructure:"write_timeout"`
}

// MySQLConfig 数据库配置
type MySQLConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DBName       string `yaml:"dbname"`
	MaxOpenConns int    `yaml:"max_open_conns" mapstructure:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns" mapstructure:"max_idle_conns"`
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
	ExpireHours int    `yaml:"expire_hours" mapstructure:"expire_hours"`
}

type Database struct {
	Mysql MySQLConfig `yaml:"mysql"`
	Redis RedisConfig `yaml:"redis"`
}

// Config 总配置结构体，嵌套所有子配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Services ServicesConfig `yaml:"services"`
	Database Database       `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Logger   Logger         `yaml:"log"`
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

// LoadConfig 加载配置文件并返回配置对象
// 这个函数简化了配置加载过程，默认加载config.yaml
func LoadConfig() (*Config, error) {
	err := InitConfig("config/config.yaml")
	if err != nil {
		// 尝试当前目录
		err = InitConfig("./config.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %v", err)
		}
	}
	return GetConfig(), nil
}
