// AI providers the agent can use with the user's own key.
export interface Preset {
  label: string
  kind: "anthropic" | "openai" | "fake"
  base_url: string
  model: string
  models: string[]
  note?: string
  keyOptional?: boolean
  devOnly?: boolean
}

export const PRESETS: Record<string, Preset> = {
  anthropic: {
    label: "Anthropic (Claude)", kind: "anthropic", base_url: "", model: "claude-opus-5-5",
    models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"],
    note: "Get a key at platform.claude.com. Leave the base URL empty unless you use a gateway.",
  },
  openai: { label: "OpenAI", kind: "openai", base_url: "https://api.openai.com/v1", model: "", models: [], note: "Use a model that supports tool calling." },
  deepseek: { label: "DeepSeek", kind: "openai", base_url: "https://api.deepseek.com/v1", model: "deepseek-chat", models: ["deepseek-chat"] },
  openrouter: { label: "OpenRouter", kind: "openai", base_url: "https://openrouter.ai/api/v1", model: "", models: [], note: "Pick a model that supports tools, e.g. anthropic/claude-sonnet-5.5." },
  qwen: { label: "Qwen (通义千问)", kind: "openai", base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus", models: ["qwen-plus", "qwen-max", "qwen-turbo"] },
  moonshot: { label: "Moonshot / Kimi", kind: "openai", base_url: "https://api.moonshot.cn/v1", model: "", models: [] },
  ollama: {
    label: "Ollama (local)", kind: "openai", base_url: "http://localhost:11434/v1", model: "", models: [], keyOptional: true,
    note: "Runs on your machine; no key needed. Choose a model with tool support. In Docker, use host.docker.internal instead of localhost.",
  },
  custom: { label: "Other OpenAI-compatible API", kind: "openai", base_url: "", model: "", models: [], keyOptional: true },
  fake: { label: "Demo model (offline)", kind: "fake", base_url: "", model: "demo", models: [], keyOptional: true, devOnly: true,
    note: "Understands “add <name> <amount> [type] [card]”; anything else shows this month's statistics. For trying Paylist without a key." },
}
