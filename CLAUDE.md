# CLAUDE.md

## Project Overview
Go-based Slack bot that automatically records channel messages to Google Sheets with chronological ordering, thread support, and duplicate prevention.

## Architecture
- `main.go`: HTTP server and event routing
- `internal/slack/`: Slack API client with retry logic and caching  
- `internal/sheets/`: Google Sheets API client with batch operations
- `internal/config/`: Environment configuration management

## Key Features
- **Auto-recording**: Records all channel messages to dedicated sheets
- **Thread support**: Captures thread replies with parent references
- **Duplicate prevention**: Prevents multiple processing of same events
- **Batch operations**: Writes messages in chronological order
- **Reset command**: `@bot reset` clears sheet and reprocesses all history
- **API resilience**: 4-attempt retry with 1s/2s/3s backoff for all API calls

## Code Style
- **Indentation**: Tabs (4-space display)
- **Encoding**: UTF-8, LF endings, trim trailing whitespace
- **Go**: Run `go fmt` after changes
- **Build**: Use `go build -o build/slack-bot .` (not root directory)
- **Rate limits**: Built-in delays and retry logic for API calls
- **Git commit message**: Must be one line

## Development Commands
- Build: `make build` or `go build -o build/slack-bot .`
- Deploy: `make deploy` (auto-deploy with file watching)
- Test: `make test`
- Service: `make status/start/stop/restart/logs`
