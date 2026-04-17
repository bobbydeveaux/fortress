# Fortress — High-Level Design (HLD)

**Version:** 1.0
**Status:** Draft

---

## 1. System Overview

Fortress is a Go CLI tool with three runtime modes:

1. **Scan mode** (`fortress scan`) — ingests a codebase, generates embeddings, and stores them in a local vector DB
2. **MCP mode** (`fortress serve`) — exposes the knowledge base as an MCP server for AI assistants
3. **Web mode** (`fortress serve --ui`) — serves a wiki + chat web application

All modes share the same underlying SQLite vector database ("Jor-El").

---

## 2. Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        SCAN PIPELINE                            │
│                                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐  │
│  │ Scanner  │───▶│ Chunker  │───▶│ Embedder │───▶│  Store   │  │
│  │          │    │          │    │          │    │          │  │
│  │ walkdir  │    │ per-type │    │ Ollama / │    │ SQLite + │  │
│  │ git log  │    │ strategy │    │ OpenAI   │    │ vec ext  │  │
│  └──────────┘    └──────────┘    └──────────┘    └────┬─────┘  │
│                                                        │        │
│                                              ┌─────────▼──────┐ │
│                                              │ Doc Generator  │ │
│                                              │ (markdown)     │ │
│                                              └────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                                  │
                           .fortress/
                           ├── jor-el.db        ← SQLite vector DB
                           └── docs/            ← generated markdown
                               ├── index.md
                               └── categories/

                                  │
            ┌─────────────────────┴─────────────────────┐
            │                                           │
            ▼                                           ▼
┌───────────────────────┐               ┌───────────────────────┐
│     MCP SERVER        │               │      WEB SERVER       │
│  fortress serve       │               │  fortress serve --ui  │
│                       │               │                       │
│  stdio transport      │               │  HTTP :8080           │
│  ┌─────────────────┐  │               │  ┌─────────────────┐  │
│  │ search          │  │               │  │   Wiki View     │  │
│  │ list_categories │  │               │  │   (browse docs) │  │
│  │ get_document    │  │               │  ├─────────────────┤  │
│  │ get_stats       │  │               │  │   Chat View     │  │
│  └────────┬────────┘  │               │  │   (RAG + LLM)   │  │
└───────────┼───────────┘               └──────────┬──────────┘
            │                                      │
            └──────────────┬───────────────────────┘
                           │
                    ┌──────▼──────┐
                    │ Query Engine│
                    │             │
                    │ vector sim  │
                    │ FTS5 hybrid │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  jor-el.db  │
                    └─────────────┘
                           │
                   (optional upload)
                           │
                    ┌──────▼──────┐
                    │  GCS / S3   │
                    └─────────────┘
```

---

## 3. Component Overview

### 3.1 Scanner

Walks the filesystem recursively from the given root path. For each file:
- Detects file type and programming language from extension and content sniffing
- Assigns a category (code, docs, config, infra, git)
- For directories containing a `.git` folder, extracts git metadata (last commit SHA, remote URL, commit log)
- Emits `Document` structs downstream to the Chunker
- Maintains ignore patterns from config (globbing)

### 3.2 Chunker

Receives `Document` structs and splits them into `Chunk` structs. Chunking strategy varies by file type:

| File Type | Strategy |
|-----------|----------|
| Source code | Split at function/class/method boundaries using regex heuristics |
| Markdown | Split at H1/H2/H3 headings |
| Config (YAML/TOML/JSON) | Keep whole file as one chunk |
| Git history | Group commits into windows of ~20 commits or by date |
| Plain text | Split at paragraph boundaries |

Each chunk is enriched with metadata: file path, repo, language, line range, category.

### 3.3 Embedder

Converts each `Chunk`'s text content into a dense vector representation (embedding). Two backends:

- **OllamaEmbedder**: Calls `POST http://localhost:11434/api/embeddings` with the configured model (default: `nomic-embed-text`, 768 dimensions)
- **OpenAIEmbedder**: Calls OpenAI's embedding API (`text-embedding-3-small`, 1536 dimensions)

Both implement a common `Embedder` interface. Chunks are processed in batches with a configurable worker pool. A progress bar tracks throughput.

### 3.4 Store (SQLite-vec)

Persists documents and their embeddings. Provides:
- **Write path**: Insert/update documents, chunks, and embedding vectors
- **Read path**: Vector similarity search (cosine), full-text search (FTS5), document retrieval by ID/path
- **Sync state**: Stores per-repo last-scanned commit SHA and per-file content hashes for incremental scans

### 3.5 Doc Generator

After each scan, generates human-readable markdown documentation from the indexed content:
- Analyses category distribution, most important files (by reference count), key patterns
- Writes `index.md` (master index) and `categories/<name>.md` (per-category pages)
- Documents are re-generated on every scan — they are always current

### 3.6 Query Engine

Shared by MCP server and Web server. Accepts a query string and returns ranked `Result` structs:
1. Embed the query using the same embedder used during scan
2. Perform cosine similarity search against the embeddings in SQLite-vec
3. Optionally blend with FTS5 keyword search (hybrid scoring)
4. Return top-k chunks with source metadata

### 3.7 MCP Server

Implements the Model Context Protocol over stdio. Claude Code (and compatible clients) add Fortress to their MCP config and can then call Jor-El's tools in any conversation. The server is stateless — it opens the DB read-only on each request.

### 3.8 Web Server

A Go HTTP server serving:
- **Wiki routes** (`/`, `/categories/:name`, `/repos/:name`, `/files/*path`) — render markdown pages as HTML
- **Search route** (`/search`) — semantic + FTS hybrid search results
- **Chat route** (`/chat`) — SSE endpoint streaming LLM responses
- **Static assets** — CSS, minimal JS (htmx)

No JavaScript build step. Templates are Go `html/template` with htmx for dynamic interactions.

### 3.9 RAG Pipeline (Chat)

Used by the Web UI chat interface:

```
User question
     │
     ▼
Embed question (Embedder)
     │
     ▼
Vector search → top-k chunks (Query Engine)
     │
     ▼
Build LLM prompt:
  "You are Jor-El, an expert on this codebase.
   Use the following context to answer the question.
   Context: [chunk 1] [chunk 2] ... [chunk k]
   Question: [user question]
   Cite sources as [file:line]."
     │
     ▼
Stream response from LLM (Ollama or Claude API)
     │
     ▼
Render response with clickable source citations
```

### 3.10 Cloud Storage

Pluggable storage backend for the `jor-el.db` file:
- `LocalStorage` — default, reads/writes to local filesystem
- `GCSStorage` — uploads to / downloads from Google Cloud Storage
- `S3Storage` — uploads to / downloads from AWS S3

Used by `fortress scan --upload` (write) and `fortress serve --db <uri>` (read).

---

## 4. Tech Stack Decisions

### Go
- Single binary, cross-platform (`GOOS/GOARCH` matrix in CI)
- Goroutines are the natural primitive for the scan/embed pipeline
- Excellent SQLite driver ecosystem
- Fast compile times for tight iteration

### SQLite + sqlite-vec
- Zero external processes — the DB is just a file
- `sqlite-vec` extension adds efficient HNSW-based vector similarity search
- `FTS5` extension (built into SQLite) provides full-text search for hybrid queries
- The DB file can be trivially copied, uploaded, and versioned

### Ollama (default embedder)
- Runs locally on macOS/Linux/Windows
- `nomic-embed-text` is a high-quality, fast embedding model
- No API keys, no cost, fully offline
- Users likely already have Ollama installed

### htmx (Web UI)
- Server-rendered HTML with small htmx attributes for dynamic behaviour
- No Node.js, no webpack, no `npm install`
- Chat streaming via Server-Sent Events (SSE) — natively supported by htmx

### GCS/S3 (Cloud Storage)
- Storing and syncing a SQLite file is the simplest possible cloud strategy
- No schema migrations, no query routing, no connection pooling
- Teams can share a knowledge base by having CI upload the DB after each scan

---

## 5. Data Flow

### 5.1 Scan Flow

```
fortress scan ./my-repos
    │
    ├─▶ Load config (fortress.yaml)
    ├─▶ Open DB (create if not exists)
    │
    ├─▶ Scanner: walk filesystem
    │       ├─ Detect file types
    │       ├─ Check content hash vs DB (skip unchanged)
    │       ├─ Extract git metadata for .git repos
    │       └─ Emit Document stream
    │
    ├─▶ Chunker: Document → []Chunk
    │
    ├─▶ Embedder: []Chunk → []Chunk (with Embedding field populated)
    │       └─ Worker pool (N goroutines → Ollama/OpenAI API)
    │
    ├─▶ Store: upsert documents, chunks, embeddings
    │       └─ Update scan_state (commit SHA, timestamps)
    │
    └─▶ Doc Generator: write .fortress/docs/
```

### 5.2 Query Flow (MCP / Web / CLI)

```
Query string
    │
    ├─▶ Embedder: query → vector
    ├─▶ Store: cosine similarity search (top-k)
    ├─▶ Store: FTS5 keyword search (optional)
    ├─▶ Merge & rank results
    └─▶ Return []Result{chunk, score, metadata}
```

---

## 6. Freshness Strategy

Keeping Jor-El current is critical — a stale knowledge base is worse than no knowledge base.

### Layer 1: Incremental Scan (Manual / Scheduled)
- Track `last_commit_sha` per repo in the `scan_state` table
- On re-scan: `git diff --name-only <last_sha>..HEAD` → only changed files processed
- For non-git directories: compare `content_hash` of each file vs stored value
- Deleted files are removed from the DB

### Layer 2: Watch Mode (Real-Time)
- `fortress watch` uses `fsnotify` to receive OS filesystem events
- On file create/modify/delete: immediately re-chunk and re-embed that file
- Suitable for running as a background daemon during development

### Layer 3: CI Integration
- `fortress scan --ci --upload gcs://bucket/fortress.db` runs in the CI pipeline
- Triggers on merge to main (or any configured branch)
- Uploads the fresh DB to cloud storage
- Team members' local `fortress serve --db gcs://...` instances pick up the new DB on restart

---

## 7. MCP Integration Design

Fortress implements the [Model Context Protocol](https://modelcontextprotocol.io) over **stdio transport**, which is the standard for local MCP servers used with Claude Code.

### Tool Definitions

| Tool | Input | Output |
|------|-------|--------|
| `search` | `query: string, limit?: int` | Array of `{content, file, repo, category, score, line_start, line_end}` |
| `list_categories` | (none) | Array of `{name, description, file_count, chunk_count}` |
| `get_document` | `path: string` | `{content, metadata, chunks[]}` |
| `get_stats` | (none) | `{repos, files, chunks, categories, last_scan, db_size_mb}` |

### Claude Code Configuration

Users add to their Claude Code `settings.json`:

```json
{
  "mcpServers": {
    "fortress": {
      "command": "fortress",
      "args": ["serve"],
      "env": {}
    }
  }
}
```

---

## 8. Web UI Architecture

```
GET /                     → category index (list of all categories)
GET /categories/:name     → category page (rendered markdown + file list)
GET /repos/:name          → repository page (files, stats, recent commits)
GET /files/*path          → file page (source content with chunk highlights)
GET /search?q=...         → search results page
GET /chat                 → chat page
POST /api/search          → JSON search API (htmx fragment target)
GET  /api/chat/stream     → SSE stream for chat responses
```

Server-sent events (SSE) power the chat stream. htmx handles fragment swapping without a JavaScript framework.

---

## 9. Cloud Deployment Model

```
Developer machine              CI/CD Pipeline            Cloud Storage
       │                             │                        │
fortress scan ./repos          fortress scan --ci       GCS/S3 Bucket
       │                       --upload gcs://...            │
       ▼                             │                        │
.fortress/jor-el.db                  └──── upload ──────────▶│
                                                              │
                                                              │
Team member machine                                          │
       │                                                      │
fortress serve --ui                                           │
  --db gcs://...   ◀──────────── download on startup ────────┘
       │
  http://localhost:8080
```

The DB is treated as an artifact, not a live service. It is downloaded on startup and served locally. This is the simplest model that supports team sharing.

---

## 10. Security Considerations

| Concern | Mitigation |
|---------|------------|
| API keys (OpenAI, Claude, GCS) | Read from environment variables only; never written to config files or DB |
| Sensitive source code | Nothing leaves the machine unless cloud storage is explicitly configured |
| Web UI exposure | Binds to `localhost` only by default; user must explicitly bind to `0.0.0.0` for network access |
| Cloud DB access | Uses standard cloud SDK credential chains (ADC for GCS, env vars for S3); bucket permissions managed by user |
| Prompt injection via code | LLM prompts treat retrieved chunks as data, not instructions; system prompt clearly delimits context |
