package gemini

import "strings"

const (
	generateContentAction       = ":generateContent"
	streamGenerateContentAction = ":streamGenerateContent"
)

// ParseGeminiPath extracts the model name and streaming flag from a Gemini
// generateContent / streamGenerateContent URL path.
func ParseGeminiPath(path string) (model string, stream bool, ok bool) {
	path = strings.TrimSpace(path)
	if q := strings.IndexByte(path, '?'); q >= 0 {
		path = path[:q]
	}
	action := ""
	switch {
	case strings.HasSuffix(path, streamGenerateContentAction):
		action = streamGenerateContentAction
		stream = true
	case strings.HasSuffix(path, generateContentAction):
		action = generateContentAction
	default:
		return "", false, false
	}
	rest := strings.TrimSuffix(path, action)
	const marker = "/models/"
	idx := strings.LastIndex(rest, marker)
	if idx < 0 {
		return "", stream, false
	}
	model = strings.TrimPrefix(strings.TrimSpace(rest[idx+len(marker):]), "models/")
	if model == "" {
		return "", stream, false
	}
	return model, stream, true
}
