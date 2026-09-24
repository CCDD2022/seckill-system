package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigUsesPathEnvAndSecretFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "custom.yaml")
	configBody := []byte("database:\n  mysql:\n    host: from-file\n    password: \"\"\nmq:\n  password: \"\"\njwt:\n  secret: \"\"\n")
	if err := os.WriteFile(configPath, configBody, 0600); err != nil {
		t.Fatal(err)
	}
	passwordFile := filepath.Join(dir, "mysql-password")
	if err := os.WriteFile(passwordFile, []byte("from-file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)
	t.Setenv("DATABASE_MYSQL_HOST", "from-env")
	t.Setenv("DATABASE_MYSQL_PASSWORD_FILE", passwordFile)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Mysql.Host != "from-env" {
		t.Fatalf("env host override = %q, want from-env", cfg.Database.Mysql.Host)
	}
	if cfg.Database.Mysql.Password != "from-file-secret" {
		t.Fatalf("secret file override was not applied")
	}
}

func TestLoadConfigRejectsMissingSecretFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("jwt:\n  secret: placeholder\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)
	t.Setenv("JWT_SECRET_FILE", filepath.Join(dir, "missing"))
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected missing secret file to fail startup")
	}
}
