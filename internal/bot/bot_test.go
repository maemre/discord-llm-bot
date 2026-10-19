package bot

import (
	"context"
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
}

func (f *fakeDiscord) ChannelMessage(channelID, messageID string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	return f.stored[messageID], nil
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

func TestAnswersReplyWithReferencedContext(t *testing.T) {
	b, d, l := setup("False.")
	d.stored["42"] = &discordgo.Message{
		Content: "the moon is cheese",
		Author:  &discordgo.User{Username: "bob"},
	}
	m := msg("<@!999>   is this real?")
	m.MessageReference = &discordgo.MessageReference{MessageID: "42", ChannelID: "c"}
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
	for _, want := range []string{"the moon is cheese", "@bob", "Question from @alice: is this real?"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt %q missing %q", user, want)
		}
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
