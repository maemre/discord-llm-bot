package bot

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// captureLogs redirects the default slog logger for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestGuildTracker(t *testing.T) {
	logs := captureLogs(t)
	g := NewGuildTracker()
	g.Ready(nil, &discordgo.Ready{Guilds: []*discordgo.Guild{{ID: "1"}}})
	g.Create(nil, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "1", Name: "old"}})
	g.Create(nil, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "2", Name: "new"}})
	g.Delete(nil, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "1", Unavailable: true}})
	g.Delete(nil, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "2"}, BeforeDelete: &discordgo.Guild{Name: "new"}})

	for _, want := range []string{
		`msg="server available" guild=1 guild_name=old`,
		`msg="added to server" guild=2 guild_name=new`,
		`msg="server unavailable" guild=1`,
		`msg="removed from server" guild=2 guild_name=new`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %q:\n%s", want, logs)
		}
	}
}

func TestInteractionLog(t *testing.T) {
	for _, logContent := range []bool{false, true} {
		logs := captureLogs(t)
		b, _, _ := setup("False.")
		b.LogContent = logContent
		b.HandleMessage(context.Background(), msg("<@999> secret question"))

		out := logs.String()
		if !strings.Contains(out, `msg=interaction`) || !strings.Contains(out, "outcome=answered") {
			t.Errorf("missing interaction line:\n%s", out)
		}
		if got := strings.Contains(out, "secret question"); got != logContent {
			t.Errorf("LogContent=%v but question in logs = %v:\n%s", logContent, got, out)
		}
	}
}
