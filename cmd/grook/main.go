// Command grook runs the Discord bot.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"

	"github.com/maemre/discord-llm-bot/internal/bot"
	"github.com/maemre/discord-llm-bot/internal/config"
	"github.com/maemre/discord-llm-bot/internal/llm"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// A missing .env is fine: in Docker the environment comes from compose.
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return err
	}
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentMessageContent

	// Registered before Open so the first READY and its GUILD_CREATEs are seen.
	guilds := bot.NewGuildTracker()
	s.AddHandler(guilds.Ready)
	s.AddHandler(guilds.Create)
	s.AddHandler(guilds.Delete)

	if err := s.Open(); err != nil {
		return err
	}
	defer s.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The bot's own user ID is only known after Open, so the handler is added here.
	b := bot.New(s, llm.New(cfg.OpenRouterAPIKey, cfg.OpenRouterModel), s.State.User.ID, bot.DefaultCooldown)
	b.LogContent = cfg.LogContent
	s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) {
		b.HandleMessage(ctx, m.Message)
	})

	slog.Info("connected", "user", s.State.User.Username, "model", cfg.OpenRouterModel)
	<-ctx.Done()
	slog.Info("shutting down")
	return nil
}
