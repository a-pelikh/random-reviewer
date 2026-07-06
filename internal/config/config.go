package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot      Bot      `yaml:"bot"`
	Postgres Postgres `yaml:"postgres"`
}

type Bot struct {
	Token  string `yaml:"token"`
	ApiURL string `yaml:"api_url"`
	Secret string `yaml:"secret"`
}

type Postgres struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	DB       string `yaml:"db"`
	Port     uint16 `yaml:"port"`
}

func (p *Postgres) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", p.User, p.Password, p.Host, p.Port, p.DB)
}

func New() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		slog.Error("no .env file found")
	}

	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "configs/random-reviewer.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}
