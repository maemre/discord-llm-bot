# grook

A parody of the "@grok is this true?" meme. Tag **@grook** in Discord, usually as a reply to someone else's message, and it fact-checks it with an LLM through [OpenRouter](https://openrouter.ai).

grook also reads some earlier messages for context. If the mention is a reply, it reads up to the last 3 messages in the reply chain. Otherwise it reads the last 3 messages in the channel or thread.

## Discord setup

1. Create an application at https://discord.com/developers/applications.
2. Under **Bot**, click **Reset Token** and put the token in `.env` as `DISCORD_TOKEN`.
3. On the same page, under **Privileged Gateway Intents**, turn on **Message Content Intent**. Without it, grook can't read the messages it uses for context.
4. Under **OAuth2 → URL Generator**, select the `bot` scope and the permissions below, then open the generated URL to invite the bot.

### OAuth2 permissions

| Permission | Why |
|---|---|
| View Channels | see the channels it is mentioned in |
| Send Messages | post answers |
| Send Messages in Threads | answer inside threads |
| Read Message History | fetch reply chains and recent channel messages for context |
| Add Reactions | react with ⏳ when a user is on cooldown |

Permissions integer: `274877975616`. Invite URL, with your application ID filled in:

```
https://discord.com/oauth2/authorize?client_id=<APPLICATION_ID>&scope=bot&permissions=274877975616
```

## Configuration

Copy `.env.example` to `.env`:

| Variable | |
|---|---|
| `DISCORD_TOKEN` | required |
| `OPENROUTER_API_KEY` | required, from https://openrouter.ai/keys |
| `OPENROUTER_MODEL` | optional, defaults to `minimax/minimax-m3.1-flash:free` |
| `LOG_CONTENT` | optional; `true` adds question and answer text to the logs (off by default) |

## Running

```sh
./run.sh                 # build the image and (re)start the "grook" container with podman
podman logs -f grook     # follow logs
go run ./cmd/grook       # or run directly without a container
```

Logs show one `interaction` line per mention (outcome, timing and context size, but no message text unless `LOG_CONTENT=true`), plus the servers grook is in and when it's added to or removed from one.

`compose.yaml` is also provided for hosts that use `docker compose`.
