// Package config reads the bot's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

const DefaultModel = "minimax/minimax-m3.1-flash:free"

type Config struct {
	DiscordToken     string
	OpenRouterAPIKey string
	OpenRouterModel  string
	// LogContent includes question and answer text in interaction logs.
	LogContent bool
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
	if v := os.Getenv("LOG_CONTENT"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("LOG_CONTENT: %w", err))
		}
		cfg.LogContent = b
	}
	if cfg.DiscordToken == "" {
		errs = append(errs, errors.New("DISCORD_TOKEN is not set"))
	}
	if cfg.OpenRouterAPIKey == "" {
		errs = append(errs, errors.New("OPENROUTER_API_KEY is not set"))
	}
	return cfg, errors.Join(errs...)
}
