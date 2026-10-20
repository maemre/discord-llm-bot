package bot

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/maemre/discord-llm-bot/internal/llm"
)

const selfID = "999"

type fakeDiscord struct {
	mu        sync.Mutex
	sent      []*discordgo.MessageSend
	reactions []string
	stored    map[string]*discordgo.Message
	history   []*discordgo.Message // newest first, like the Discord API
	historyN  int
}

func (f *fakeDiscord) ChannelMessage(channelID, messageID string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	return f.stored[messageID], nil
}

func (f *fakeDiscord) ChannelMessages(_ string, limit int, _, _, _ string, _ ...discordgo.RequestOption) ([]*discordgo.Message, error) {
	f.historyN = limit
	return slices.Clone(f.history[:min(limit, len(f.history))]), nil
}

func (f *fakeDiscord) ChannelTyping(string, ...discordgo.RequestOption) error { return nil }

func (f *fakeDiscord) ChannelMessageSendComplex(_ string, data *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, data)
	return &discordgo.Message{}, nil
}

func (f *fakeDiscord) MessageReactionAdd(_, _, emoji string, _ ...discordgo.RequestOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, emoji)
	return nil
}

type fakeLLM struct {
	reply string
	err   error
	got   []llm.Message
}

func (f *fakeLLM) Complete(_ context.Context, msgs []llm.Message) (string, error) {
	f.got = msgs
	return f.reply, f.err
}

func msg(content string) *discordgo.Message {
	return &discordgo.Message{
		ID:        "1",
		ChannelID: "c",
		Content:   content,
		Author:    &discordgo.User{ID: "u1", Username: "alice"},
	}
}

func setup(reply string) (*Bot, *fakeDiscord, *fakeLLM) {
	d := &fakeDiscord{stored: map[string]*discordgo.Message{}}
	l := &fakeLLM{reply: reply}
	return New(d, l, selfID, time.Minute), d, l
}

func TestIgnoresMessagesWithoutMention(t *testing.T) {
	b, d, _ := setup("x")
	b.HandleMessage(context.Background(), msg("hello there"))
	if len(d.sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(d.sent))
	}
}

func TestIgnoresBots(t *testing.T) {
	b, d, _ := setup("x")
	m := msg("<@999> hi")
	m.Author.Bot = true
	b.HandleMessage(context.Background(), m)
	if len(d.sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(d.sent))
	}
}

func replyTo(id string) *discordgo.MessageReference {
	return &discordgo.MessageReference{MessageID: id, ChannelID: "c"}
}

func TestAnswersReplyWithReplyChain(t *testing.T) {
	b, d, l := setup("False.")
	// Chain: 39 <- 40 <- 41 <- 42 <- mention. Only 40..42 should be used.
	d.stored["39"] = &discordgo.Message{Content: "too old", Author: &discordgo.User{Username: "eve"}}
	d.stored["40"] = &discordgo.Message{Content: "what's the moon made of", Author: &discordgo.User{Username: "carol"}, MessageReference: replyTo("39")}
	d.stored["41"] = &discordgo.Message{Content: "rock, obviously", Author: &discordgo.User{ID: selfID, Username: "grook"}, MessageReference: replyTo("40")}
	d.stored["42"] = &discordgo.Message{Content: "the moon is cheese", Author: &discordgo.User{Username: "bob"}, MessageReference: replyTo("41")}
	m := msg("<@!999>   is this real?")
	m.MessageReference = replyTo("42")
	b.HandleMessage(context.Background(), m)

	if len(d.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(d.sent))
	}
	reply := d.sent[0]
	if reply.Content != "False." {
		t.Errorf("content = %q", reply.Content)
	}
	if reply.AllowedMentions == nil || len(reply.AllowedMentions.Parse) != 0 {
		t.Errorf("allowed mentions not locked down: %+v", reply.AllowedMentions)
	}
	user := l.got[1].Content
	for _, want := range []string{
		"[@carol] what's the moon made of\n[@grook (you)] rock, obviously",
		"Message being asked about, from @bob:\n\"\"\"\nthe moon is cheese",
		"Question from @alice: is this real?",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt %q missing %q", user, want)
		}
	}
	if strings.Contains(user, "too old") {
		t.Errorf("prompt includes message beyond the chain limit: %q", user)
	}
	if d.historyN != 0 {
		t.Error("reply should not fetch channel history")
	}
}

func TestUsesChannelHistoryWhenNotReply(t *testing.T) {
	b, d, l := setup("ok")
	d.history = []*discordgo.Message{
		{Content: "third", Author: &discordgo.User{Username: "c"}},
		{Content: "second", Author: &discordgo.User{Username: "b"}},
		{Content: "first", Author: &discordgo.User{Username: "a"}},
		{Content: "too old", Author: &discordgo.User{Username: "z"}},
	}
	b.HandleMessage(context.Background(), msg("<@999> who is right?"))

	if d.historyN != contextMessages {
		t.Errorf("requested %d messages, want %d", d.historyN, contextMessages)
	}
	user := l.got[1].Content
	if !strings.Contains(user, "[@a] first\n[@b] second\n[@c] third") {
		t.Errorf("history missing or out of order: %q", user)
	}
	if strings.Contains(user, "too old") || strings.Contains(user, "Message being asked about") {
		t.Errorf("unexpected prompt content: %q", user)
	}
}

func TestBareMentionUsesDefaultQuestion(t *testing.T) {
	b, _, l := setup("ok")
	b.HandleMessage(context.Background(), msg("<@999>"))
	if !strings.Contains(l.got[1].Content, defaultQuestion) {
		t.Errorf("prompt %q missing default question", l.got[1].Content)
	}
}

func TestCooldownReacts(t *testing.T) {
	b, d, _ := setup("ok")
	b.HandleMessage(context.Background(), msg("<@999> one"))
	b.HandleMessage(context.Background(), msg("<@999> two"))
	if len(d.sent) != 1 || len(d.reactions) != 1 {
		t.Errorf("sent=%d reactions=%d, want 1 and 1", len(d.sent), len(d.reactions))
	}
}

func TestLLMErrorStillReplies(t *testing.T) {
	b, d, l := setup("")
	l.err = llm.ErrRateLimited
	b.HandleMessage(context.Background(), msg("<@999> hi"))
	if len(d.sent) != 1 || !strings.Contains(d.sent[0].Content, "rate-limited") {
		t.Errorf("unexpected replies: %+v", d.sent)
	}
}

func TestTruncateCountsRunes(t *testing.T) {
	s := strings.Repeat("é", 2500)
	got := truncate(s, maxMessageRunes)
	if n := len([]rune(got)); n != maxMessageRunes {
		t.Errorf("got %d runes, want %d", n, maxMessageRunes)
	}
}
