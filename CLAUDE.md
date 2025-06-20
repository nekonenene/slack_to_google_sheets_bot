# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
Go-based Slack app that records channel posts to Google Sheets.

**Key Requirements:**
- Record Slack channel posts to Google Sheets with specific format
- Handle Slack API rate limiting with sleep intervals
- Support Firebase or TiDB Cloud as database
- Auto-create sheets when bot joins channels
- Fetch historical posts from oldest to newest

## Sheet Format
Each channel gets its own sheet with headers:
- No., Date, Handle, Real Name, Content, Thread No.
- Mention format: @username, Channel format: #channelname
- Records sorted by post time (ascending)

## Code Style (from .editorconfig)
- **Go files**: Tab indentation (4-space width)
- **General**: UTF-8 encoding, LF line endings, trim trailing whitespace
- Insert final newline in all files

## Development Status
This is a new project - no Go code has been implemented yet. When implementing:
- Initialize with `go mod init`
- Follow standard Go project structure
- Implement rate limiting for Slack API calls
- Consider using goroutines for concurrent processing while respecting rate limits
