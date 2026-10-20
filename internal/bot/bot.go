// Package bot answers questions when users @mention the bot on Discord.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	// How many earlier messages (reply chain or channel history) go into the prompt.
	contextMessages = 3
)

// Discord is the subset of *discordgo.Session the bot uses, so tests can fake it.
type Discord interface {
	ChannelMessage(channelID, messageID string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string, options ...discordgo.RequestOption) ([]*discordgo.Message, error)
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

	// LogContent adds question and answer text to interaction logs. Off by
	// default because it puts other people's messages in the logs.
	LogContent bool

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
	question = resolveMentions(question, m.Mentions)
	log := slog.With("guild", m.GuildID, "channel", m.ChannelID, "message", m.ID,
		"user", m.Author.ID, "user_name", m.Author.Username)
	if b.LogContent {
		log = log.With("question", question)
	}

	if !b.allow(m.Author.ID) {
		log.Info("interaction", "outcome", "cooldown")
		if err := b.discord.MessageReactionAdd(m.ChannelID, m.ID, cooldownEmoji); err != nil {
			log.Warn("adding cooldown reaction", "err", err)
		}
		return
	}

	stopTyping := b.keepTyping(m.ChannelID)
	defer stopTyping()

	target, history := b.conversation(m, log)
	prompt := buildPrompt(question, m.Author, target, history, b.selfID)

	llmCtx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()
	start := b.now()
	answer, err := b.llm.Complete(llmCtx, prompt)
	llmTime := b.now().Sub(start)

	outcome := "answered"
	switch {
	case errors.Is(err, llm.ErrRateLimited):
		outcome = "rate_limited"
		answer = "I've been rate-limited by my corporate overlords. Ask me again in a minute."
	case err != nil:
		outcome = "llm_error"
		answer = "My circuits are fried. Try again later."
	}

	reply := truncate(answer, maxMessageRunes)
	_, sendErr := b.discord.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:   reply,
		Reference: m.Reference(),
		// Never let LLM output ping anyone, and don't ping the asker either.
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
	})
	if sendErr != nil {
		outcome = "send_failed"
		err = errors.Join(err, sendErr)
	}

	// One line per interaction; anything but a clean answer is a warning.
	contextMsgs := len(history)
	if target != nil {
		contextMsgs++
	}
	attrs := []any{
		"outcome", outcome,
		"reply", target != nil,
		"context_msgs", contextMsgs,
		"llm_ms", llmTime.Milliseconds(),
		"answer_chars", utf8.RuneCountInString(reply),
	}
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	if b.LogContent {
		attrs = append(attrs, "answer", reply)
	}
	level := slog.LevelInfo
	if outcome != "answered" {
		level = slog.LevelWarn
	}
	log.Log(ctx, level, "interaction", attrs...)
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

// conversation gathers up to contextMessages earlier messages for the prompt.
// For a reply it walks up the reply chain, and target is the message replied
// to. Otherwise it takes the latest messages before m in the channel or thread.
// history is oldest first.
func (b *Bot) conversation(m *discordgo.Message, log *slog.Logger) (target *discordgo.Message, history []*discordgo.Message) {
	if isReply(m) {
		var chain []*discordgo.Message // newest first
		for cur := m; len(chain) < contextMessages; {
			parent := b.parent(cur, log)
			if parent == nil {
				break
			}
			chain = append(chain, parent)
			cur = parent
		}
		if len(chain) == 0 {
			return nil, nil
		}
		slices.Reverse(chain)
		return chain[len(chain)-1], chain[:len(chain)-1]
	}

	msgs, err := b.discord.ChannelMessages(m.ChannelID, contextMessages, m.ID, "", "")
	if err != nil {
		log.Warn("fetching channel history", "err", err)
		return nil, nil
	}
	slices.Reverse(msgs) // Discord returns newest first
	return nil, msgs
}

func isReply(m *discordgo.Message) bool {
	ref := m.MessageReference
	return ref != nil && ref.MessageID != "" && ref.Type != discordgo.MessageReferenceTypeForward
}

// parent returns the message m replies to, or nil.
func (b *Bot) parent(m *discordgo.Message, log *slog.Logger) *discordgo.Message {
	if !isReply(m) {
		return nil
	}
	if m.ReferencedMessage != nil {
		return m.ReferencedMessage
	}
	channelID := m.MessageReference.ChannelID
	if channelID == "" {
		channelID = m.ChannelID
	}
	p, err := b.discord.ChannelMessage(channelID, m.MessageReference.MessageID)
	if err != nil {
		log.Warn("fetching referenced message", "err", err)
		return nil
	}
	return p
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
