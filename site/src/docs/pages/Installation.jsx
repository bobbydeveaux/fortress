import DocPage, { Code, Callout } from '../DocPage.jsx'

export default function Installation() {
  return (
    <DocPage slug="installation">
      <h2>Prerequisites</h2>
      <ul>
        <li><strong>Go 1.22+</strong> &mdash; Fortress is built with Go</li>
        <li><strong>Ollama</strong> (recommended) &mdash; for local embeddings with <code>nomic-embed-text</code></li>
        <li>Or an <strong>OpenAI API key</strong> for cloud-based embeddings</li>
      </ul>

      <h2>Install via Go</h2>
      <p>
        Fortress requires CGO and the <code>sqlite_fts5</code> build tag for full-text search:
      </p>
      <Code>{`CGO_ENABLED=1 go install -tags sqlite_fts5 github.com/bobbydeveaux/fortress@latest`}</Code>

      <Callout type="warning">
        <strong>The build tag is required.</strong> Without <code>-tags sqlite_fts5</code>, full-text
        search won't work and you'll get a "no such module: fts5" error.
      </Callout>

      <p>Make sure the Go bin directory is in your PATH:</p>
      <Code>{`export PATH=$PATH:$(go env GOPATH)/bin`}</Code>

      <h2>Install from source</h2>
      <Code>{`# Clone the repo
git clone https://github.com/bobbydeveaux/fortress.git
cd fortress

# Build the binary (CGO + FTS5 required)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o fortress .

# Move to your PATH
sudo mv fortress /usr/local/bin/

# Verify
fortress --help`}</Code>

      <h2>Set up Ollama (recommended)</h2>
      <p>
        Fortress uses Ollama by default for local embeddings. Install Ollama and pull the embedding model:
      </p>
      <Code>{`# Install Ollama (macOS)
brew install ollama

# Start Ollama
ollama serve

# Pull the embedding model
ollama pull nomic-embed-text`}</Code>

      <Callout type="info">
        <strong>Using OpenAI instead?</strong> Set your API key in <code>fortress.yaml</code> or
        as an environment variable: <code>export OPENAI_API_KEY=sk-...</code>, then
        set <code>embedder: openai</code> in your config.
      </Callout>

      <h2>Verify installation</h2>
      <Code>{`$ fortress --help
Fortress — codebase knowledge base for AI tools

Usage:
  fortress [command]

Available Commands:
  scan     Scan and index a codebase
  search   Search the knowledge base
  serve    Start MCP or web UI server
  stats    Show database statistics
  watch    Watch for changes and re-scan
  forget   Remove a repo from the index`}</Code>

      <p>You're ready to go. Head to <a href="/docs/quickstart">Quick Start</a> to index your first codebase.</p>
    </DocPage>
  )
}
