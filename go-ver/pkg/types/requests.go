package types

type OpenAIChatRequest struct {
	Model    string        `json:"model"`
	Messages []OpenAIInput `json:"messages"`
	Stream   bool          `json:"stream"`
}

type OpenAIInput struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ClaudeMessageRequest struct {
	Model    string                 `json:"model"`
	Messages []map[string]any       `json:"messages"`
	Stream   bool                   `json:"stream"`
	Tools    []map[string]any       `json:"tools"`
	System   any                    `json:"system"`
	Extra    map[string]interface{} `json:"-"`
}
