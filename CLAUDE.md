# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Fortress** — a Go CLI tool that recursively scans codebases, generates embeddings, and stores them in a local SQLite vector database ("Jor-El"). Queryable via MCP server (for Claude Code), CLI, and a web UI with wiki + chat.

Full documentation lives in `docs/`:
- [`docs/PRD.md`](docs/PRD.md) — what and why (vision, user stories, feature requirements)
- [`docs/HLD.md`](docs/HLD.md) — system architecture, component overview, data flows, tech stack decisions
- [`docs/LLD.md`](docs/LLD.md) — package structure, interfaces, structs, SQL schema, algorithms, config schema

## Commands

```bash
# Build
go build -o fortress .

# Run all tests
go test ./...

# Run integration tests (requires Ollama running locally)
go test ./integration/...

# Run a single test
go test -run TestChunker ./internal/chunker/...

# Run linter
golangci-lint run

# Scan a directory
./fortress scan ./path/to/repos

# Start MCP server (for Claude Code)
./fortress serve

# Start web UI (wiki + chat) on :8080
./fortress serve --ui
```

## Architecture in Brief

The scan pipeline flows: **Scanner → Chunker → Embedder → Store → Doc Generator**

Three runtime modes share the same SQLite DB (`.fortress/jor-el.db`):
1. `fortress scan` — ingestion pipeline
2. `fortress serve` — MCP stdio server (Claude Code integration)
3. `fortress serve --ui` — HTTP server (wiki + chat web UI)

Embeddings default to **Ollama** (`nomic-embed-text`) locally; OpenAI is a configurable alternative. The vector DB uses **sqlite-vec** extension. Chat uses a RAG pipeline (embed query → vector search → LLM with context).

## Key Design Decisions

- **Pure-Go SQLite driver** (`modernc.org/sqlite`) for cross-platform builds without CGO — but sqlite-vec may require CGO; evaluate at implementation time
- **htmx** for the web UI — no JavaScript build step, no Node dependency
- **Incremental scans** track `last_commit_sha` per repo and `content_hash` per file to avoid re-embedding unchanged content
- **Cloud storage** treats the DB file as an artifact (upload/download to GCS or S3) — not a live remote connection

## Config

Default config file: `fortress.yaml` in the working directory. All fields documented in `docs/LLD.md §10`.
