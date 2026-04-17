package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Embedder string       `mapstructure:"embedder"`
	Ollama   OllamaConfig `mapstructure:"ollama"`
	OpenAI   OpenAIConfig `mapstructure:"openai"`
	ChatLLM  string       `mapstructure:"chat_llm"`
	Claude   ClaudeConfig `mapstructure:"claude"`

	Ignore []string `mapstructure:"ignore"`

	ChunkSize    int `mapstructure:"chunk_size"`
	ChunkOverlap int `mapstructure:"chunk_overlap"`

	DBPath   string `mapstructure:"db_path"`
	DocsPath string `mapstructure:"docs_path"`

	CloudStorage string `mapstructure:"cloud_storage"`

	UIPort int    `mapstructure:"ui_port"`
	UIBind string `mapstructure:"ui_bind"`

	EmbedWorkers   int `mapstructure:"embed_workers"`
	EmbedBatchSize int `mapstructure:"embed_batch_size"`
}

type OllamaConfig struct {
	URL        string `mapstructure:"url"`
	EmbedModel string `mapstructure:"embed_model"`
	ChatModel  string `mapstructure:"chat_model"`
}

type OpenAIConfig struct {
	APIKey     string `mapstructure:"api_key"`
	EmbedModel string `mapstructure:"embed_model"`
}

type ClaudeConfig struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Embedder: "ollama",
		Ollama: OllamaConfig{
			URL:        "http://localhost:11434",
			EmbedModel: "nomic-embed-text",
			ChatModel:  "llama3.2",
		},
		OpenAI: OpenAIConfig{
			EmbedModel: "text-embedding-3-small",
		},
		ChatLLM: "ollama",
		Claude: ClaudeConfig{
			Model: "claude-sonnet-4-6",
		},
		Ignore: []string{
			".git", "node_modules", "vendor", ".venv", "__pycache__",
			"*.bin", "*.exe", "*.so", "*.dylib",
			"dist", "build", "coverage", ".fortress",
		},
		ChunkSize:      512,
		ChunkOverlap:   64,
		DBPath:         ".fortress/jor-el.db",
		DocsPath:       ".fortress/docs/",
		UIPort:         8080,
		UIBind:         "127.0.0.1",
		EmbedWorkers:   2,
		EmbedBatchSize: 32,
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	if key := viper.GetString("OPENAI_API_KEY"); key != "" {
		cfg.OpenAI.APIKey = key
	}
	if key := viper.GetString("ANTHROPIC_API_KEY"); key != "" {
		cfg.Claude.APIKey = key
	}

	return cfg, nil
}

func (c *Config) EmbedDimensions() int {
	switch c.Embedder {
	case "openai":
		return 1536
	default:
		return 768
	}
}
