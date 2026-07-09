# whoop-mcp-server

[![CI](https://github.com/Zayden16/whoop-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayden16/whoop-mcp-server/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Zayden16/whoop-mcp-server)](https://github.com/Zayden16/whoop-mcp-server/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Zayden16/whoop-mcp-server)](https://goreportcard.com/report/github.com/Zayden16/whoop-mcp-server)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

MCP server for the official [Whoop API v2](https://developer.whoop.com), written in Go.
Exposes cycles, recovery, sleep, workouts, and profile data as MCP tools.

## Tools

| Tool | Description |
|---|---|
| `get_profile` | User profile (name, email, user ID) |
| `get_body_measurement` | Height, weight, max heart rate |
| `get_cycles` | Physiological cycles (day strain, kJ, HR) for a date range |
| `get_latest_cycle` | Most recent cycle |
| `get_recoveries` | Recovery scores (recovery %, HRV, RHR, SpO2) for a date range |
| `get_recovery_for_cycle` | Recovery for a specific cycle ID |
| `get_sleep` | Sleep records (duration, stages, efficiency) for a date range |
| `get_workouts` | Workouts (sport, strain, HR zones) for a date range |
| `get_average_strain` | Average day strain over the last N days (default 7) |
| `check_auth_status` | Verify API authentication |

Dates are `YYYY-MM-DD`; `end_date` is inclusive.

## Setup

### 1. Create a Whoop developer app

At [developer.whoop.com](https://developer.whoop.com), create an app with:

- **Redirect URI:** `http://localhost:8719/callback`
- **Scopes:** `read:cycles`, `read:recovery`, `read:sleep`, `read:workout`, `read:profile`, `read:body_measurement`, `offline`

Note the client ID and client secret.

### 2. Install

Homebrew:

```sh
brew install zayden16/tap/whoop-mcp-server
```

Or download a prebuilt binary from [Releases](https://github.com/Zayden16/whoop-mcp-server/releases), or:

```sh
go install github.com/Zayden16/whoop-mcp-server@latest
```

Or build from source:

```sh
go build -o whoop-mcp-server .
```

### 3. Authorize (one time)

```sh
export WHOOP_CLIENT_ID=...
export WHOOP_CLIENT_SECRET=...
./whoop-mcp-server auth
```

Opens a browser for the OAuth flow; the token is saved to
`~/Library/Application Support/whoop-mcp/token.json` (macOS) and refreshed
automatically thereafter (rotating refresh tokens).

### 4. Register with Claude Code

```sh
claude mcp add whoop --scope user \
  -e WHOOP_CLIENT_ID=... \
  -e WHOOP_CLIENT_SECRET=... \
  -- /path/to/whoop-mcp-server
```

Or for Claude Desktop, in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "whoop": {
      "command": "/path/to/whoop-mcp-server",
      "env": {
        "WHOOP_CLIENT_ID": "...",
        "WHOOP_CLIENT_SECRET": "..."
      }
    }
  }
}
```

## Example queries

- "What's my recovery score today?"
- "Show my sleep for the past week"
- "What's my average strain over the last 7 days?"
