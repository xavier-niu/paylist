<p align="center">
  <img src="docs/icon.svg" width="96" height="96" alt="">
</p>

<h1 align="center">Paylist</h1>

<p align="center">
  <strong>Your spending, month by month: a ledger you write in, an overview that shows where it went,<br>and an agent that keeps the books for you, with your own AI key.</strong>
</p>

<p align="center">
  Self-hosted · one Go binary · SQLite · English and 中文 · light and dark
</p>

---

Paylist is a personal and household spending tracker you run yourself. Every expense is one line, with a name, a type, an amount, a date and the card it was paid with, kept month by month. You write them down in a few keystrokes; Paylist shows you the month at a glance; and an AI agent (yours, on your own key) can add, fix and answer questions about them. Agents you already use, such as Claude Code, can do the same over MCP.

## Overview

Paylist opens on the month at a glance.

- **This month's spending** against the same days of last month: ↑ CN¥801.72 (10.2%).
- **Cumulative spending** through the month, drawn over last month's, so you see whether you're ahead or behind.
- **Day by day**: a calendar shaded by how much went out each day.
- **By type** and **by card**: two donuts, with the share of each.
- **Monthly spending** for the last twelve months.

Click a day, a type or a card to list its entries right there, and click an entry to change it.

## Ledger

One month as a table, edited in place, like a spreadsheet.

- **Press N** anywhere to add an entry. The form stays open for the next one, and as you type a name it offers the ones you've used before (by pinyin too: "kf" finds 咖啡), filling in the type, card and last amount.
- **Press R** for a refund, filled in like a new entry. If a purchase from the last 120 days has the same name and amount, it is deleted (with Undo); if not, the refund is added as a negative amount.
- Click any cell to change it; Enter or Esc saves. Types and cards are colored options, found by name or pinyin; a new name is created once you confirm it.
- **Filter** by name, type, amount, date or card, each a chip you can change, and **Sort** by date, amount or name, setting the one you like as the default. Search, select rows to delete several at once, and export the month as CSV.
- The header keeps the month to date and the change from last month in view; the sidebar keeps this month's spending by type and your recent months a click away.

## Agent

Chat with an assistant that works on your ledger:

> add lunch 35 and a metro ride 4.5 today, paid by card
>
> add a coffee
>
> I got 299 back for the headphones
>
> compare this month with last month, type by type

- **Bring your own key**: Anthropic (Claude), OpenAI, DeepSeek, OpenRouter, Qwen, Moonshot, Ollama, or any other OpenAI-compatible API with tool calling. Keys are stored encrypted on your server and never sent back to the browser.
- The agent uses the same MCP tools as outside agents, and you see every call it makes, with what went in and what came back.
- Conversations are saved, so you can pick one up later.

## MCP

Paylist is an MCP server. Create a token in **Settings → Agent access** and connect Claude Code, Codex, Cursor or any MCP client:

```sh
claude mcp add --transport http paylist https://pay.example.com/mcp \
  --header "Authorization: Bearer plt_…"
```

| Tool | Does |
|---|---|
| `list_ledgers` | The user's ledgers, their own and those shared with them, with their role in each. Every other tool takes an optional `ledger` (a name or an id); without it, the ledger last open in Paylist |
| `get_profile` | A ledger's name and currency, the user's role in it and their language, and today's date |
| `list_entries` | Entries of a month or a date range, optionally of one type or card, or with names containing some text; with their count and total |
| `add_entries` | Add entries, all or none. The date defaults to today; a new type or card becomes an option |
| `update_entry` | Change the fields given |
| `delete_entries` | Delete entries by id |
| `refund` | Record a refund the way the ledger's Refund form does: a purchase from the last 120 days with the same name and amount is deleted; with several, they come back to pick from; with none, the refund is added as a negative amount under the type, card and date given |
| `suggest_entries` | Names used before that contain some text, with how each was last entered (type, card, amount), to fill in what the user leaves out |
| `list_options` | The types and cards, with their colors and how many entries use each |
| `manage_option` | Create, rename, recolor or delete a type or card; renaming carries through to every entry |
| `get_stats` | A month's total, change from the same days of the month before, daily totals, totals by type and by card, and the months before it |
| `list_months` | Every month with entries, with its total and count |

Everything you can do with your ledger in Paylist, an agent can do through these tools. Account settings, members, model keys and tokens stay in the app.

A token reaches only its owner's ledger, and can be revoked at any time.

## Ledgers, shared

Everyone starts with a personal ledger and can make more, such as one for the household or a trip, each with its own types, cards and currency; the sidebar lists them and switches between them. A ledger's owner can share it, in **Settings → This ledger**, with other people on the server: an **editor** changes it as the owner does, a **viewer** only reads it. The agent and MCP work on the ledger that's open, or one named by its name.

One server holds everyone, each with their own ledgers, model and key. The first person to open a new server becomes its admin and adds the others in **Settings → Accounts**.

## Get started

### Try the demo

You need [Go](https://go.dev/dl/) 1.27+ and [Node.js](https://nodejs.org/) 20+ (or Docker, which `make` uses to build the web app when Node is missing).

```sh
make demo
```

Open <http://localhost:8080> and sign in as `demo@admin.com` / `admin`. The demo account comes with six months of made-up spending, and an offline **Demo model** for the agent that needs no key (it understands `add <name> <amount> [type] [card]`). `make demo-reset` starts it over. The demo password is short, so keep it on your own machine.

### Run it with Docker

```sh
docker run -d --name paylist --restart unless-stopped \
  -p 8080:8080 -v paylist-data:/data xavierniu/paylist
```

Open <http://localhost:8080> and create the admin account; whoever opens a new server first becomes its admin, so do it right away, or create it up front with `-e PAYLIST_ADMIN_EMAIL=you@example.com -e PAYLIST_ADMIN_PASSWORD='a long password'`.

Behind an HTTPS proxy (Caddy, nginx), also set `-e PAYLIST_SECURE_COOKIES=1 -e PAYLIST_PUBLIC_URL=https://pay.example.com`.

Images are built for amd64 and arm64 from every push to `main` (`latest`) and every `v*` tag. To build your own: `docker build -t paylist .`

### Back up

Everything lives in the data directory: `paylist.db`, and `secret.key`, which encrypts the stored API keys. Back them up together; without `secret.key`, everyone has to enter their API key again.

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
| `PAYLIST_DEMO_SEED=1` | off | Fill the `PAYLIST_ADMIN_EMAIL` account's empty ledger with demo data |

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

The agent's model runs on the server, with the key stored there: each turn opens an MCP client session to an in-memory MCP server for the signed-in user, lists its tools, and loops (model → tool calls → results) until the model answers, streaming text and tool activity to the browser as newline-delimited JSON. With Claude it runs at medium effort with prompt caching, uses the server-side refusal fallback where the model supports it, and replays Claude's earlier replies exactly as written.
