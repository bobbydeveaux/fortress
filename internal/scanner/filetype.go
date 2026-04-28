package scanner

import (
	"path/filepath"
	"strings"
)

var languageExtensions = map[string]string{
	".go":     "go",
	".py":     "python",
	".js":     "javascript",
	".ts":     "typescript",
	".tsx":    "typescript",
	".jsx":    "javascript",
	".rs":     "rust",
	".java":   "java",
	".rb":     "ruby",
	".php":    "php",
	".c":      "c",
	".cpp":    "cpp",
	".h":      "c",
	".hpp":    "cpp",
	".cs":     "csharp",
	".swift":  "swift",
	".kt":     "kotlin",
	".scala":  "scala",
	".lua":    "lua",
	".r":      "r",
	".pl":     "perl",
	".sh":     "shell",
	".bash":   "shell",
	".zsh":    "shell",
	".fish":   "shell",
	".ps1":    "powershell",
	".sql":    "sql",
	".html":   "html",
	".htm":    "html",
	".css":    "css",
	".scss":   "scss",
	".less":   "less",
	".vue":    "vue",
	".svelte": "svelte",
	".dart":   "dart",
	".ex":     "elixir",
	".exs":    "elixir",
	".erl":    "erlang",
	".hs":     "haskell",
	".ml":     "ocaml",
	".clj":    "clojure",
	".tf":     "terraform",
	".proto":  "protobuf",
	".graphql":"graphql",
	".gql":    "graphql",
}

var configExtensions = map[string]bool{
	".yaml": true, ".yml": true, ".toml": true, ".json": true,
	".env": true, ".ini": true, ".cfg": true, ".conf": true,
	".properties": true,
}

var configFilenames = map[string]bool{
	"Dockerfile": true, "Makefile": true, "Rakefile": true,
	"Gemfile": true, "Procfile": true, "Vagrantfile": true,
	".gitignore": true, ".dockerignore": true, ".editorconfig": true,
	".eslintrc": true, ".prettierrc": true, "tsconfig.json": true,
	"package.json": true, "go.mod": true, "go.sum": true,
	"Cargo.toml": true, "Cargo.lock": true, "requirements.txt": true,
	"pyproject.toml": true, "setup.py": true, "setup.cfg": true,
	"pom.xml": true, "build.gradle": true, "build.sbt": true,
	"CMakeLists.txt": true, "docker-compose.yml": true,
	"docker-compose.yaml": true, ".github": true,
	"Jenkinsfile": true, ".travis.yml": true, ".circleci": true,
}

func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := languageExtensions[ext]; ok {
		return lang
	}

	base := filepath.Base(path)
	switch base {
	case "Dockerfile":
		return "dockerfile"
	case "Makefile", "GNUmakefile":
		return "makefile"
	case "Jenkinsfile":
		return "groovy"
	}

	return ""
}

func DetectFileType(path string, language string) FileType {
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)

	if ext == ".md" || ext == ".mdx" || ext == ".rst" || ext == ".txt" || ext == ".adoc" {
		return FileTypeMarkdown
	}

	if configExtensions[ext] || configFilenames[base] {
		return FileTypeConfig
	}

	if language != "" {
		return FileTypeCode
	}

	return FileTypeData
}

var dirCategoryMap = map[string]Category{
	"api": CategoryAPI, "apis": CategoryAPI, "handlers": CategoryAPI, "routes": CategoryAPI, "controllers": CategoryAPI,
	"infra": CategoryInfra, "infrastructure": CategoryInfra, "terraform": CategoryInfra, "deploy": CategoryInfra, "k8s": CategoryInfra, "kubernetes": CategoryInfra, "helm": CategoryInfra,
	"frontend": CategoryFrontend, "ui": CategoryFrontend, "web": CategoryFrontend, "client": CategoryFrontend, "components": CategoryFrontend, "pages": CategoryFrontend,
	"test": CategoryTesting, "tests": CategoryTesting, "testing": CategoryTesting, "spec": CategoryTesting, "specs": CategoryTesting, "__tests__": CategoryTesting,
	"docs": CategoryDocs, "documentation": CategoryDocs, "doc": CategoryDocs,
	"config": CategoryConfig, "configs": CategoryConfig, "configuration": CategoryConfig,
	"migrations": CategoryMigrations, "migrate": CategoryMigrations, "schema": CategoryMigrations,
}

var testFileSuffixes = []string{"_test.go", ".test.js", ".test.ts", ".spec.js", ".spec.ts", "_spec.rb"}

func DetectCategory(relPath string, language string, ft FileType) Category {
	lower := strings.ToLower(relPath)
	parts := strings.Split(lower, string(filepath.Separator))

	for _, part := range parts {
		if cat, ok := dirCategoryMap[part]; ok {
			return cat
		}
	}

	for _, suffix := range testFileSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return CategoryTesting
		}
	}

	if ft == FileTypeMarkdown {
		return CategoryDocs
	}
	if ft == FileTypeConfig {
		return CategoryConfig
	}

	return CategoryUnknown
}
