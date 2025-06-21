# CLAUDE.md

## Project Overview
Go-based Slack bot that automatically records channel messages to Google Sheets with chronological ordering, thread support, and duplicate prevention.

## Architecture
- `main.go`: HTTP server and event routing
- `internal/slack/`: Slack API client with retry logic and caching  
- `internal/sheets/`: Google Sheets API client with batch operations
- `internal/config/`: Environment configuration management
- `internal/progress/`: Progress tracking for resumable channel history retrieval

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
- **File endings**: All files must end with a newline character
- **Documentation**: All exported functions and methods must have godoc comments
- **Go formatting**: Always run `go fmt` after code changes
- **Build output**: All binaries must be built to `build/` directory using `go build -o build/slack-bot .`
- **Rate limits**: Built-in delays and retry logic for API calls
- **Git commit message**: Must be one line
