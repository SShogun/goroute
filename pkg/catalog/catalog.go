package catalog

import (
	"strings"
)

type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`     // "openai", "antigravity"
	Category    string `json:"category"` // "frontier", "inference", "gateway", "local", "oauth"
	BaseURL     string `json:"base_url"`
	AuthType    string `json:"auth_type"` // "apikey", "oauth", "none"
	Description string `json:"description"`
}

var Registry = []CatalogEntry{
	// Frontier & Big Labs
	{
		ID:          "openai",
		Name:        "OpenAI",
		Type:        "openai",
		Category:    "frontier",
		BaseURL:     "https://api.openai.com/v1",
		AuthType:    "apikey",
		Description: "Official OpenAI GPT-4o, o1, o3-mini models",
	},
	{
		ID:          "gemini",
		Name:        "Google AI Studio (Gemini)",
		Type:        "openai",
		Category:    "frontier",
		BaseURL:     "https://generativelanguage.googleapis.com/v1beta/openai",
		AuthType:    "apikey",
		Description: "Google Gemini 2.5 Pro / Flash OpenAI-compatible endpoint",
	},
	{
		ID:          "xai",
		Name:        "xAI (Grok)",
		Type:        "openai",
		Category:    "frontier",
		BaseURL:     "https://api.x.ai/v1",
		AuthType:    "apikey",
		Description: "Grok 2 / Grok 3 models by xAI",
	},
	{
		ID:          "mistral",
		Name:        "Mistral AI",
		Type:        "openai",
		Category:    "frontier",
		BaseURL:     "https://api.mistral.ai/v1",
		AuthType:    "apikey",
		Description: "Mistral Large, Codestral, Pixtral",
	},
	{
		ID:          "deepseek",
		Name:        "DeepSeek",
		Type:        "openai",
		Category:    "frontier",
		BaseURL:     "https://api.deepseek.com/v1",
		AuthType:    "apikey",
		Description: "DeepSeek V3 and DeepSeek R1 reasoning models",
	},

	// Ultra-Fast Inference & GPU Clouds
	{
		ID:          "nvidia",
		Name:        "NVIDIA NIM",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://integrate.api.nvidia.com/v1",
		AuthType:    "apikey",
		Description: "NVIDIA NIM microservices & hosted open models (Llama, DeepSeek, Mistral)",
	},
	{
		ID:          "groq",
		Name:        "Groq",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.groq.com/openai/v1",
		AuthType:    "apikey",
		Description: "Ultra-low latency LPU inference for Llama 3, DeepSeek, Gemma",
	},
	{
		ID:          "cerebras",
		Name:        "Cerebras",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.cerebras.ai/v1",
		AuthType:    "apikey",
		Description: "Wafer-scale high-throughput inference (2,000+ tokens/sec)",
	},
	{
		ID:          "sambanova",
		Name:        "SambaNova",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.sambanova.ai/v1",
		AuthType:    "apikey",
		Description: "High-speed full-precision open models",
	},
	{
		ID:          "together",
		Name:        "Together AI",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.together.xyz/v1",
		AuthType:    "apikey",
		Description: "Fast serverless inference engine for leading open-source models",
	},
	{
		ID:          "fireworks",
		Name:        "Fireworks AI",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.fireworks.ai/inference/v1",
		AuthType:    "apikey",
		Description: "Production-speed inference platform",
	},
	{
		ID:          "deepinfra",
		Name:        "DeepInfra",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.deepinfra.com/v1/openai",
		AuthType:    "apikey",
		Description: "Pay-as-you-go serverless inference for LLMs and embeddings",
	},
	{
		ID:          "hyperbolic",
		Name:        "Hyperbolic",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.hyperbolic.xyz/v1",
		AuthType:    "apikey",
		Description: "Decentralized GPU cluster inference",
	},
	{
		ID:          "nebius",
		Name:        "Nebius AI Studio",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.studio.nebius.ai/v1",
		AuthType:    "apikey",
		Description: "NVIDIA H100/H200 cloud inference",
	},
	{
		ID:          "siliconflow",
		Name:        "SiliconFlow",
		Type:        "openai",
		Category:    "inference",
		BaseURL:     "https://api.siliconflow.cn/v1",
		AuthType:    "apikey",
		Description: "High concurrency inference platform (Qwen, DeepSeek, Llama)",
	},

	// Aggregators & Gateways
	{
		ID:          "openrouter",
		Name:        "OpenRouter",
		Type:        "openai",
		Category:    "gateway",
		BaseURL:     "https://openrouter.ai/api/v1",
		AuthType:    "apikey",
		Description: "Universal gateway aggregating 200+ models",
	},
	{
		ID:          "tokenrouter",
		Name:        "TokenRouter",
		Type:        "openai",
		Category:    "gateway",
		BaseURL:     "https://api.tokenrouter.io/v1",
		AuthType:    "apikey",
		Description: "Multi-model router and failover gateway",
	},
	{
		ID:          "agentrouter",
		Name:        "AgentRouter",
		Type:        "openai",
		Category:    "gateway",
		BaseURL:     "https://api.agentrouter.org/v1",
		AuthType:    "apikey",
		Description: "Unified AI gateway for coding agents",
	},
	{
		ID:          "apinexbond",
		Name:        "ApinexBond",
		Type:        "openai",
		Category:    "gateway",
		BaseURL:     "https://api.apinexbond.com/v1",
		AuthType:    "apikey",
		Description: "Enterprise AI endpoint bridge",
	},

	// Local & Self-Hosted
	{
		ID:          "ollama",
		Name:        "Ollama (Local)",
		Type:        "openai",
		Category:    "local",
		BaseURL:     "http://localhost:11434/v1",
		AuthType:    "none",
		Description: "Local LLM server running on your machine",
	},
	{
		ID:          "lmstudio",
		Name:        "LM Studio (Local)",
		Type:        "openai",
		Category:    "local",
		BaseURL:     "http://localhost:1234/v1",
		AuthType:    "none",
		Description: "Local desktop model runner with OpenAI server",
	},
	{
		ID:          "vllm",
		Name:        "vLLM (Local / Server)",
		Type:        "openai",
		Category:    "local",
		BaseURL:     "http://localhost:8000/v1",
		AuthType:    "none",
		Description: "High-throughput PagedAttention local serving",
	},
	{
		ID:          "llamacpp",
		Name:        "llama.cpp Server",
		Type:        "openai",
		Category:    "local",
		BaseURL:     "http://localhost:8080/v1",
		AuthType:    "none",
		Description: "Native C++ GGUF model runner",
	},

	// OAuth Providers
	{
		ID:          "antigravity",
		Name:        "Google Antigravity CLI",
		Type:        "antigravity",
		Category:    "oauth",
		BaseURL:     "https://cloudcode-pa.googleapis.com",
		AuthType:    "oauth",
		Description: "Google Cloud Code Assist OAuth PKCE flow (Claude 3.7 Sonnet & Gemini 2.5)",
	},
}

// FindEntry looks up a provider preset by ID or name
func FindEntry(query string) *CatalogEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	for _, entry := range Registry {
		if strings.ToLower(entry.ID) == q || strings.ToLower(entry.Name) == q {
			return &entry
		}
	}
	return nil
}

// Search returns entries matching a keyword
func Search(keyword string) []CatalogEntry {
	if keyword == "" {
		return Registry
	}
	q := strings.ToLower(keyword)
	var matches []CatalogEntry
	for _, entry := range Registry {
		if strings.Contains(strings.ToLower(entry.ID), q) ||
			strings.Contains(strings.ToLower(entry.Name), q) ||
			strings.Contains(strings.ToLower(entry.Description), q) ||
			strings.Contains(strings.ToLower(entry.Category), q) {
			matches = append(matches, entry)
		}
	}
	return matches
}
