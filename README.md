# whoop-mcp-server

[![CI](https://github.com/Zayden16/whoop-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Zayden16/whoop-mcp-server/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Zayden16/whoop-mcp-server)](https://github.com/Zayden16/whoop-mcp-server/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Zayden16/whoop-mcp-server)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

MCP server for the official [Whoop API v2](https://developer.whoop.com), written in Go.
Exposes cycles, recovery, sleep, workouts, and profile data as MCP tools.

## Tools

All tools return JSON. Record shapes are the unmodified [Whoop API v2](https://developer.whoop.com/api/) objects, so every field the API exposes is available to the model.

| Tool | Inputs | Returns |
|---|---|---|
| `get_profile` | — | Single object: `user_id`, `email`, `first_name`, `last_name` |
| `get_body_measurement` | — | Single object: `height_meter`, `weight_kilogram`, `max_heart_rate` |
| `get_cycles` | `start_date`, `end_date`, `limit` (all optional) | Array of cycle records — one per physiological day: `start`/`end`, `score_state`, and `score` with day `strain`, `kilojoule`, `average_heart_rate`, `max_heart_rate` |
| `get_latest_cycle` | — | Single cycle record (the current/most recent day) |
| `get_recoveries` | `start_date`, `end_date`, `limit` (all optional) | Array of recovery records — one per sleep: `cycle_id`, `sleep_id`, and `score` with `recovery_score` (0–100), `resting_heart_rate`, `hrv_rmssd_milli`, `spo2_percentage`, `skin_temp_celsius` |
| `get_recovery_for_cycle` | `cycle_id` (required) | Single recovery record for that cycle |
| `get_sleep` | `start_date`, `end_date`, `limit` (all optional) | Array of sleep records: `start`/`end`, `nap` flag, and `score` with per-stage durations (light/SWS/REM/awake, in ms), `respiratory_rate`, `sleep_performance_percentage`, `sleep_efficiency_percentage`, sleep-need breakdown |
| `get_workouts` | `start_date`, `end_date`, `limit` (all optional) | Array of workout records: `sport_name`, `start`/`end`, and `score` with workout `strain`, heart rates, `kilojoule`, `distance_meter`, `altitude_gain_meter`, time-in-zone durations (`zone_durations`, ms per HR zone) |
| `get_average_strain` | `days` (optional, default 7) | Computed aggregate: `{days, cycles_counted, average_strain}` |
| `check_auth_status` | — | `{authenticated: bool}` plus the profile on success or an `error` string on failure |

Notes:

- **Dates** are `YYYY-MM-DD`; `end_date` is inclusive (internally mapped to the start of the next day, per the API's exclusive `end` semantics).
- **Collections** are date-descending (most recent first) and paginated transparently — the server follows `next_token` until `limit` records (default 25, max 100) are collected.
- **Scores** are point-in-time per record, not time-series: one strain value per cycle, one recovery per sleep. For trends, fetch a range and let the model aggregate (or use `get_average_strain` for the built-in strain average).
- A record's `score_state` can be `SCORED`, `PENDING_SCORE`, or `UNSCORABLE` — `score` is only present when `SCORED`.

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
