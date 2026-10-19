# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- **Never add Claude as a co-author on commits**: no `Co-Authored-By` trailer or other self-attribution in commit messages.
- `.env` holds real secrets and is gitignored and dockerignored. Document new variables in `.env.example`.

## Purpose

**corg** is a parody of the "@grok is this true?" meme: a Discord bot that answers when someone @mentions it, typically in a reply to another message ("@corg is this real?"). Answers come from an LLM via OpenRouter, using the model `minimax/minimax-m3.1-flash:free` by default.

## Commands

```sh
go run ./cmd/corg                        # run locally (loads .env)
go test ./...                            # add -race when touching concurrency
go test ./internal/bot -run TestName     # single test
go vet ./... && gofmt -l .
podman build -t corg .                   # docker works the same; this machine has podman
podman compose up -d --build             # or docker compose; the same file is used on the cloud VM
docker buildx build --platform linux/amd64,linux/arm64 .   # multi-arch, for the Oracle arm64 VM
```

## Stack and hosting

Go + [`discordgo`](https://github.com/bwmarrin/discordgo). OpenRouter is called with plain `net/http` (no SDK). Keep dependencies minimal. The image is a static binary (`CGO_ENABLED=0`) on distroless.

Answering @mentions requires the Discord **Gateway** WebSocket, which means a persistent process, so serverless hosting is out. The same image runs:
1. **Locally / self-hosted** via compose, or `go run`.
2. **Free-tier cloud**: an always-free VM, either Oracle Cloud Always Free (arm64) or GCP e2-micro (amd64). Render/Koyeb free instances sleep and would drop the Gateway connection.

The bot only makes outbound connections, so it needs no public URL or open port.

## Architecture

- `cmd/corg`: wiring only. It loads `.env` (if present), validates config, opens the session, then creates the bot and adds the handler. Handler registration happens *after* `Open()` because the bot's own user ID is needed. Shutdown is on SIGINT/SIGTERM.
- `internal/config`: the only place that reads env vars.
- `internal/llm`: the OpenRouter chat-completions client. It maps HTTP 429 to `ErrRateLimited` and treats an `error` object in the body as a failure, even on HTTP 200.
- `internal/bot`: the mention flow (`bot.go`), plus the persona system prompt and prompt construction (`prompt.go`). It depends on the `Discord` and `LLM` interfaces, not on `*discordgo.Session`, and tests use fakes for both. If you call a new session method, add it to the `Discord` interface.

### Mention flow (`Bot.HandleMessage`)

1. Ignore bot authors. Only an **explicit `<@ID>`/`<@!ID>` mention in the text** triggers the bot. Reply pings, `@everyone` and role mentions don't.
2. A per-user cooldown (`DefaultCooldown`) guarded by a mutex. If the user is still on cooldown, the bot reacts with ⏳ instead of answering. discordgo runs handlers concurrently, so any shared state needs locking.
3. If the message is a reply, the referenced message (`m.ReferencedMessage`, falling back to a fetch) is put into the prompt as the claim, including its embed text and attachment names. A bare "@corg" defaults to "is this true?".
4. The typing indicator is refreshed every 8s until the reply is sent. Each LLM call gets a 90s timeout.
5. The reply uses `Reference: m.Reference()` and an **empty `AllowedMentions.Parse`**, so LLM output can never ping anyone. Text is truncated by runes to 2000 characters. LLM failures still produce an in-character error reply.

### Intents

Guilds, GuildMessages, DirectMessages and the privileged **MessageContent** intent. Without MessageContent, the referenced message's content arrives empty and the reply flow breaks. The intent must also be enabled in the Developer Portal (Bot → Privileged Gateway Intents).

### Configuration

`DISCORD_TOKEN` (required), `OPENROUTER_API_KEY` (required), and `OPENROUTER_MODEL` (optional). `:free` OpenRouter models have tight rate limits, which is why the cooldown exists.
