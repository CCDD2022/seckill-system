package config

import (
	"fmt"

	"github.com/spf13/viper"
)

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
	Level      string `yaml:"level"`  // ✅ 添加 yaml 标签
	Format     string `yaml:"format"` // ✅ 添加 yaml 标签
	Output     string `yaml:"output"` // ✅ 添加 yaml 标签
	FilePath   string `yaml:"file_path" mapstructure:"file_path"`
	MaxSize    int    `yaml:"max_size" mapstructure:"max_size"`
	MaxBackups int    `yaml:"max_backups" mapstructure:"max_backups"`
	MaxAge     int    `yaml:"max_age" mapstructure:"max_age"`
}

type ServerConfig struct {
	Port         int    `yaml:"port"`
	Mode         string `yaml:"mode"`
	ReadTimeout  int    `yaml:"read_timeout" mapstructure:"read_timeout"` // ✅ 改为大写
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

// MQConfig RabbitMQ配置
type MQConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Config 总配置结构体，嵌套所有子配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Services ServicesConfig `yaml:"services"`
	Database Database       `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Logger   Logger         `yaml:"log" mapstructure:"log"`
	MQ       MQConfig       `yaml:"mq"`
}

func InitConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 读取内容
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败:%v", err)
	}

	var globalConfig Config
	if err := viper.Unmarshal(&globalConfig); err != nil {
		return nil, fmt.Errorf("解析配置文件失败:%v", err)
	}

	return &globalConfig, nil
}

// LoadConfig 加载配置文件并返回配置对象
// 这个函数简化了配置加载过程，默认加载config.yaml
func LoadConfig() (*Config, error) {
	cfg, err := InitConfig("./config/config.yaml")
	if err != nil {
		// 尝试当前目录
		cfg, err = InitConfig("./config.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %v", err)
		}
	}

	fmt.Println(cfg)
	return cfg, nil
}
