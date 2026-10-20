package bot

import (
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// GuildTracker logs which servers the bot is in, and when it is added to or
// removed from one. Discord sends GUILD_CREATE both for servers the bot was
// already in (right after READY) and for newly joined ones, so the READY guild
// list is what tells the two apart.
type GuildTracker struct {
	mu    sync.Mutex
	known map[string]bool
}

func NewGuildTracker() *GuildTracker {
	return &GuildTracker{known: make(map[string]bool)}
}

// Ready resets the known servers. It runs on every fresh (non-resumed) connection.
func (t *GuildTracker) Ready(_ *discordgo.Session, r *discordgo.Ready) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.known = make(map[string]bool, len(r.Guilds))
	for _, g := range r.Guilds {
		t.known[g.ID] = true
	}
	slog.Info("server count", "servers", len(r.Guilds))
}

func (t *GuildTracker) Create(_ *discordgo.Session, g *discordgo.GuildCreate) {
	t.mu.Lock()
	existing := t.known[g.ID]
	t.known[g.ID] = true
	t.mu.Unlock()

	attrs := []any{"guild", g.ID, "guild_name", g.Name, "members", g.MemberCount}
	if existing {
		slog.Info("server available", attrs...)
		return
	}
	slog.Info("added to server", append(attrs, "joined_at", g.JoinedAt)...)
}

func (t *GuildTracker) Delete(_ *discordgo.Session, g *discordgo.GuildDelete) {
	// Unavailable means a Discord outage, not a removal.
	if g.Unavailable {
		slog.Warn("server unavailable", "guild", g.ID)
		return
	}
	t.mu.Lock()
	delete(t.known, g.ID)
	t.mu.Unlock()

	name := ""
	if g.BeforeDelete != nil {
		name = g.BeforeDelete.Name
	}
	slog.Info("removed from server", "guild", g.ID, "guild_name", name)
}
