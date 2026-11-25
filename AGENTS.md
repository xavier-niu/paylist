# Paylist, for coding agents

Paylist is a self-hosted spending tracker: a Go server (`cmd/paylist`, `internal/`) with SQLite, a React + TypeScript web app (`web/ui`) built into the binary, an MCP server for outside agents, and an agent page that uses the same MCP tools.

## Rules

### 1. MCP can do everything the app can

Whenever a feature is added or changed in the app, MCP gets it in the same change: an agent connected over MCP (and the agent page, which uses the same tools) must be able to do whatever a person can do in Paylist with their ledgers.

For each change, check and update:

- **The tools** in `internal/server/mcp.go`: a new tool, or a new argument or action on an existing one. Reads return the same data the app shows (new fields on an entry or a statistic come through the tool's output).
- **The words agents read**: the tool's `Description` and its arguments' `jsonschema` notes, and `mcpInstructions` at the top of `mcp.go`, which says which tool does what.
- **The tests**: `internal/server/mcp_test.go` calls each ability over a real MCP connection, and `TestMCP` in `server_test.go` counts the tools.
- **The README**'s MCP table.

Left to the app on purpose: account settings (password, display name, theme, language), AI model keys, agent tokens, and the admin's management of the server's accounts. Everything about ledgers, entries, types, cards, refunds, history and statistics belongs in MCP.

## Working here

- **Checks**, all of which must pass:
  - `go vet ./... && go test ./...` from the repository root.
  - `npx tsc -b` in `web/ui`.
  - `npm run build` in `web/ui`, which writes the app the Go binary embeds.
- **Languages**: the app is in English and Chinese. Every string shown is wrapped in `t()` (or `tn()`, `tParts()`) from `web/ui/src/lib/i18n.ts`, and needs its translation in `web/ui/src/lib/zh.ts`. Server error messages the app shows go through `t()` as well.
- **The database** changes only by a new entry at the end of `migrations` in `internal/store/store.go`, never by editing an old one: existing servers upgrade by running the ones they haven't.
