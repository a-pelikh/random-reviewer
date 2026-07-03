package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Bot struct {
	Token  string `yaml:"BOT_TOKEN"`
	ApiURL string `yaml:"BOT_API_URL"`
}

func New() (*Bot, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found")
	}

	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "configs/random-reviewer.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Bot
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}
