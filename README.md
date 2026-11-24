# Paylist

**Your monthly spending in one table, with statistics, an MCP server, and an agent that works on it with your own AI key.**

Paylist is a self-hosted spending tracker. The **Ledger** page keeps a spreadsheet-like table of what you paid each month: a name, a type, an amount, a date and the card you paid with. A statistics page charts it by month, by day, by type and by card. AI agents such as Claude Code can read and change the ledger over MCP, and the built-in **Agent** page does the same with the model and key you choose.

It is one Go program with a SQLite database and the web app built in.

## What you can do

**The table**
- One month at a time; step back and forth, or jump to this month.
- Click any cell to edit it. **Type** and **Card** are dropdowns of options with colors, like a spreadsheet's; type a new name to create one.
- Add expenses in the last row: press Enter and the row clears for the next one, keeping the type, card and date.
- Filter the month, select rows to delete several at once, and download the month as CSV.

**Statistics**
- This month's total, compared with last month; the daily average; the number of entries; the largest type.
- Monthly spending for the last 12 months (click a month to open it), daily spending for the month, and totals by type and by card.
- Every chart has a table view.

**MCP**
- Paylist is an MCP server at `/mcp` (Streamable HTTP). Create a token in **Settings → Agent access** and connect Claude Code, Codex, Cursor or any MCP client.
- Tools: `list_entries`, `add_entries`, `update_entry`, `delete_entries`, `list_options`, `manage_option`, `get_stats`.

**Agent**
- Chat with an assistant that works on your table: "add lunch 35 and a metro ride 4.5, paid by card", "how much did I spend on food this month?", "rename the card Alipay to Alipay Wallet".
- You bring your own key: Anthropic (Claude), OpenAI, DeepSeek, OpenRouter, Qwen, Moonshot, Ollama or any other OpenAI-compatible API with tool calling. Keys are stored encrypted on your server and never sent back to the browser.
- The agent connects to the same MCP server that external agents use, so it has exactly the same tools. You see each tool it calls, with its input and result. Conversations are saved so you can continue them.

**Accounts**
- One server can hold a household: each person has their own table, options, model and key. The first person to open a new server becomes its admin and adds the others in **Settings → Members**.

## Try it: `make demo`

You need [Go](https://go.dev/dl/) 1.27+ and [Node.js](https://nodejs.org/) 20+ (or Docker, which `make` uses to build the web app when Node is missing).

```sh
make demo
```

Open <http://localhost:8080> and sign in as `demo@admin.com` / `admin`. The demo account starts with six months of made-up spending, and the **Agent** page offers an offline **Demo model** that needs no key: it understands `add <name> <amount> [type] [card]` and answers anything else with this month's statistics. Choose a real provider in **Settings → AI model** to talk to a real agent.

The demo data lives in `/tmp/paylist-demo`; `make demo-reset` deletes it so the next `make demo` starts fresh. The demo password is short and the demo model is on, so keep it on your own machine.

## Deploy with Docker

```sh
docker build -t paylist .
docker run -d --name paylist --restart unless-stopped \
  -p 8080:8080 -v paylist-data:/data paylist
```

Open <http://localhost:8080>. The first visit asks you to create the admin account. Whoever opens a new server first becomes its admin, so do it right away, or create the admin before anyone can connect with `-e PAYLIST_ADMIN_EMAIL=you@example.com -e PAYLIST_ADMIN_PASSWORD='a long password'`.

Behind an HTTPS proxy (Caddy, nginx), also set `-e PAYLIST_SECURE_COOKIES=1 -e PAYLIST_PUBLIC_URL=https://pay.example.com`.

The CI workflow (`.github/workflows/docker.yml`) runs the tests and, on pushes to `main`, builds a multi-arch image and pushes it to Docker Hub (`DOCKERHUB_TOKEN`, and optionally `DOCKERHUB_USERNAME` and `DOCKERHUB_REPO`, as repository secrets).

### Back up

Everything is in the data directory: `paylist.db` and `secret.key`, which encrypts the stored API keys. Back them up together; without `secret.key`, everyone has to enter their API key again.

## Connect an agent over MCP

1. In **Settings → Agent access**, name a token and create it. Copy it: it is shown once.
2. Add the server to your agent. For Claude Code:
   ```sh
   claude mcp add --transport http paylist https://pay.example.com/mcp \
     --header "Authorization: Bearer plt_…"
   ```
   Other clients take the same address and header in their MCP settings.

| Tool | Does |
|---|---|
| `list_entries` | Entries of a month (the current one by default) or a date range, optionally one type, one card, or names containing a query; with their count and total |
| `add_entries` | Add entries, all or none: `{entries: [{name, amount, date?, type?, card?}]}`. The date defaults to today; a new type or card becomes an option |
| `update_entry` | Change the fields given: `{id, name?, amount?, date?, type?, card?}` |
| `delete_entries` | Delete by id: `{ids}` |
| `list_options` | The type and card options, with their colors and how many entries use each |
| `manage_option` | Create, rename, recolor or delete an option: `{kind, action, name, new_name?, color?}`. Renaming renames it on every entry; deleting clears it from them |
| `get_stats` | A month's total, count, daily average, change from the month before, daily totals, totals by type and by card, and the monthly totals before it |

A token reaches only its owner's table. Revoke it any time in the same place.

## Configuration

| Flag / env | Default | Meaning |
|---|---|---|
| `-addr` / `PAYLIST_ADDR` | `127.0.0.1:8080` (`0.0.0.0:8080` in Docker) | Listen address |
| `-data` / `PAYLIST_DATA` | `./data` (`/data` in Docker) | Holds `paylist.db` and `secret.key` |
| `PAYLIST_SECRET` | generated into `secret.key` | 32-byte key (hex or base64) that encrypts stored API keys |
| `PAYLIST_ADMIN_EMAIL`, `PAYLIST_ADMIN_PASSWORD` | unset | Create this admin on first start (only while there are no accounts) |
| `PAYLIST_SECURE_COOKIES=1` | off | Mark the session cookie `Secure`; set it when serving over HTTPS |
| `PAYLIST_PUBLIC_URL` | from each request | How people reach the server; set it behind a proxy |
| `-static` / `PAYLIST_STATIC_DIR` | embedded | Serve the web app from a directory instead, such as `web/dist` |
| `PAYLIST_DEV_FAKE=1` | off | Offer the offline demo model. For demos and tests only |
| `PAYLIST_DEMO_SEED=1` | off | Fill the `PAYLIST_ADMIN_EMAIL` account's empty table with demo data |

Accounts can also be managed from the command line:

```sh
paylist user add -email you@example.com [-name "You"] [-admin]
paylist user list
paylist user passwd -email you@example.com
paylist user delete -email you@example.com
```

## Development

```sh
make build        # the web app (web/ui → web/dist), then ./paylist with it embedded
make test         # go vet and the Go tests
make demo         # the demo server on :8080
make ui-dev       # the web app with hot reload on :5173, using the API on :8080
```

```
cmd/paylist/        main: serve, the `user` subcommands, and the demo data
internal/store/     SQLite: users, sessions, tokens, entries, options, statistics, agent chats; API-key encryption
internal/server/    HTTP API, sessions and CSRF guard, the MCP server (mcp.go), the agent endpoint (agentchat.go)
internal/agent/     the agent loop: the model, with the MCP server's tools through an MCP client session
internal/llm/       model adapters: Anthropic (official Go SDK), OpenAI-compatible (Chat Completions with tools), the demo model
web/ui/             the web app: React, TypeScript, Tailwind CSS and shadcn/ui, built by Vite into web/dist
web/embed.go        embeds web/dist into the binary
```

The agent page's model runs on the server, with the key stored there: each turn opens an MCP client session to an in-memory MCP server for the signed-in user, lists its tools, and loops (model → tool calls → results) until the model answers, streaming text and tool activity to the browser as newline-delimited JSON. With Claude, it uses adaptive thinking at medium effort, prompt caching, and the server-side refusal fallback where the model supports it; Claude's replies are stored as Claude wrote them and sent back unchanged in later turns.
