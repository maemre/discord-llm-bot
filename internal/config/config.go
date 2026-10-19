// Package config reads the bot's settings from the environment.
package config

import (
	"errors"
	"os"
)

const DefaultModel = "minimax/minimax-m3.1-flash:free"

type Config struct {
	DiscordToken     string
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// Load reads the configuration from environment variables. Callers that want
// .env support should load it into the environment first.
func Load() (Config, error) {
	cfg := Config{
		DiscordToken:     os.Getenv("DISCORD_TOKEN"),
		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:  os.Getenv("OPENROUTER_MODEL"),
	}
	if cfg.OpenRouterModel == "" {
		cfg.OpenRouterModel = DefaultModel
	}

	var errs []error
	if cfg.DiscordToken == "" {
		errs = append(errs, errors.New("DISCORD_TOKEN is not set"))
	}
	if cfg.OpenRouterAPIKey == "" {
		errs = append(errs, errors.New("OPENROUTER_API_KEY is not set"))
	}
	return cfg, errors.Join(errs...)
}
