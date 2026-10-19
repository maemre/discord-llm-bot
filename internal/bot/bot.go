// Package bot answers questions when users @mention the bot on Discord.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/maemre/discord-llm-bot/internal/llm"
)

const (
	maxMessageRunes = 2000
	llmTimeout      = 90 * time.Second
	// Discord's typing indicator lasts about 10 seconds.
	typingInterval  = 8 * time.Second
	DefaultCooldown = 10 * time.Second
	cooldownEmoji   = "⏳"
)

// Discord is the subset of *discordgo.Session the bot uses, so tests can fake it.
type Discord interface {
	ChannelMessage(channelID, messageID string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelTyping(channelID string, options ...discordgo.RequestOption) error
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
	MessageReactionAdd(channelID, messageID, emojiID string, options ...discordgo.RequestOption) error
}

type LLM interface {
	Complete(ctx context.Context, msgs []llm.Message) (string, error)
}

type Bot struct {
	discord  Discord
	llm      LLM
	selfID   string
	cooldown time.Duration
	now      func() time.Time

	mu       sync.Mutex
	lastSeen map[string]time.Time // user ID -> time of last answered question
}

func New(d Discord, l LLM, selfID string, cooldown time.Duration) *Bot {
	return &Bot{
		discord:  d,
		llm:      l,
		selfID:   selfID,
		cooldown: cooldown,
		now:      time.Now,
		lastSeen: make(map[string]time.Time),
	}
}

// HandleMessage answers m if it explicitly @mentions the bot. discordgo calls
// handlers on separate goroutines, so this may block and run concurrently.
func (b *Bot) HandleMessage(ctx context.Context, m *discordgo.Message) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	question, ok := b.stripSelfMention(m.Content)
	if !ok {
		return
	}
	log := slog.With("channel", m.ChannelID, "message", m.ID, "user", m.Author.ID)

	if !b.allow(m.Author.ID) {
		if err := b.discord.MessageReactionAdd(m.ChannelID, m.ID, cooldownEmoji); err != nil {
			log.Warn("adding cooldown reaction", "err", err)
		}
		return
	}

	stopTyping := b.keepTyping(m.ChannelID)
	defer stopTyping()

	ref := b.referencedMessage(m, log)
	prompt := buildPrompt(resolveMentions(question, m.Mentions), m.Author, ref)

	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()
	answer, err := b.llm.Complete(ctx, prompt)
	switch {
	case errors.Is(err, llm.ErrRateLimited):
		log.Warn("llm rate limited")
		answer = "I've been rate-limited by my corporate overlords. Ask me again in a minute."
	case err != nil:
		log.Error("llm call failed", "err", err)
		answer = "My circuits are fried. Try again later."
	}

	_, err = b.discord.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:   truncate(answer, maxMessageRunes),
		Reference: m.Reference(),
		// Never let LLM output ping anyone, and don't ping the asker either.
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
	})
	if err != nil {
		log.Error("sending reply", "err", err)
	}
}

// stripSelfMention reports whether content mentions the bot and returns the
// content with those mentions removed. Only an explicit mention in the text
// counts; reply pings and @everyone/role mentions do not.
func (b *Bot) stripSelfMention(content string) (string, bool) {
	found := false
	for _, tok := range []string{"<@" + b.selfID + ">", "<@!" + b.selfID + ">"} {
		if strings.Contains(content, tok) {
			found = true
			content = strings.ReplaceAll(content, tok, "")
		}
	}
	return strings.Join(strings.Fields(content), " "), found
}

// allow enforces the per-user cooldown and records the attempt if allowed.
func (b *Bot) allow(userID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if last, ok := b.lastSeen[userID]; ok && now.Sub(last) < b.cooldown {
		return false
	}
	b.lastSeen[userID] = now
	return true
}

func (b *Bot) referencedMessage(m *discordgo.Message, log *slog.Logger) *discordgo.Message {
	if m.ReferencedMessage != nil {
		return m.ReferencedMessage
	}
	if m.MessageReference == nil || m.MessageReference.MessageID == "" {
		return nil
	}
	channelID := m.MessageReference.ChannelID
	if channelID == "" {
		channelID = m.ChannelID
	}
	ref, err := b.discord.ChannelMessage(channelID, m.MessageReference.MessageID)
	if err != nil {
		log.Warn("fetching referenced message", "err", err)
		return nil
	}
	return ref
}

// keepTyping shows the typing indicator until the returned func is called.
func (b *Bot) keepTyping(channelID string) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(typingInterval)
		defer t.Stop()
		for {
			if err := b.discord.ChannelTyping(channelID); err != nil {
				slog.Debug("sending typing indicator", "err", err)
			}
			select {
			case <-done:
				return
			case <-t.C:
			}
		}
	}()
	return func() { close(done) }
}

// truncate limits s to max runes (Discord counts characters, not bytes).
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
