package media

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

var (
	// reDataURL captures standard and multiline base64 data URLs.
	// Matches data:image/<subtype>;base64,<base64_payload> allowing linebreaks within the base64 payload.
	reDataURL = regexp.MustCompile(`data:image\/[a-zA-Z0-9.+_-]+;base64,[A-Za-z0-9+/=]+(?:\r?\n[ \t]*[A-Za-z0-9+/=]+)*`)
)

// CleanAndValidateBase64 strips whitespace/newlines, normalizes URL-safe base64,
// repairs missing or malformed padding, and verifies that the payload decodes to at least a minimal
// binary header (>= 8 bytes). It rejects truncated, empty, or corrupted base64 payloads.
func CleanAndValidateBase64(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	// Strip all whitespace characters (\r, \n, spaces, tabs)
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, raw)

	// An image base64 payload must decode to at least 8 bytes (minimal header),
	// which requires at least 11 base64 data characters.
	unpadded := strings.TrimRight(clean, "=")
	if len(unpadded) < 11 {
		return "", false
	}

	unpadded = strings.ReplaceAll(strings.ReplaceAll(unpadded, "-", "+"), "_", "/")

	// Recompute canonical RFC 4648 padding
	switch len(unpadded) % 4 {
	case 0:
		clean = unpadded
	case 2:
		clean = unpadded + "=="
	case 3:
		clean = unpadded + "="
	case 1:
		// Length % 4 == 1 is impossible in valid base64 (a 6-bit quantum cannot form an 8-bit byte)
		return "", false
	}

	// Verify decoding and minimal image header size
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil || len(decoded) < 8 {
		return "", false
	}

	return clean, true
}

// ExtractDataURLMatches finds all valid data:image/...;base64,... URLs in text,
// including multiline data URLs, while avoiding greedily consuming subsequent plain text.
func ExtractDataURLMatches(text string) []string {
	var matches []string
	idx := 0
	for idx < len(text) {
		start := strings.Index(text[idx:], "data:image/")
		if start == -1 {
			break
		}
		absStart := idx + start
		comma := strings.Index(text[absStart:], ";base64,")
		if comma == -1 || comma > 64 { // mime type sanity limit
			idx = absStart + 11
			continue
		}

		payloadStart := absStart + comma + len(";base64,")
		payloadEnd := payloadStart
		hasPadding := false

		for payloadEnd < len(text) {
			b := text[payloadEnd]
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '+' || b == '/' || b == '-' || b == '_' {
				if hasPadding {
					break
				}
				payloadEnd++
			} else if b == '=' {
				hasPadding = true
				payloadEnd++
				if payloadEnd < len(text) && text[payloadEnd] == '=' {
					payloadEnd++
				}
				break
			} else if b == '\r' || b == '\n' {
				if hasPadding {
					break
				}
				next := payloadEnd
				for next < len(text) && (text[next] == ' ' || text[next] == '\t' || text[next] == '\r' || text[next] == '\n') {
					next++
				}
				if next < len(text) && isBase64Char(text[next]) {
					payloadEnd = next
				} else {
					break
				}
			} else {
				break
			}
		}

		candidate := text[absStart:payloadEnd]
		if ParseImage(candidate) != nil {
			matches = append(matches, candidate)
			idx = payloadEnd
		} else {
			idx = absStart + 11
		}
	}
	return matches
}

func isBase64Char(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '+' || b == '/' || b == '=' || b == '-' || b == '_'
}

// ExtractedImage represents an image extracted from a message or tool output.
type ExtractedImage struct {
	MediaType string
	Data      string
	URL       string
}

// ToDataURL returns the image as a standard data URL (data:<mime>;base64,<data>) or its original URL.
func (img *ExtractedImage) ToDataURL() string {
	if img.URL != "" {
		return img.URL
	}
	mType := img.MediaType
	if mType == "" {
		mType = "image/png"
	}
	return fmt.Sprintf("data:%s;base64,%s", mType, img.Data)
}

// ToChatContentBlock converts the image to a Bifrost ChatContentBlockTypeImage.
func (img *ExtractedImage) ToChatContentBlock(cc *schemas.CacheControl) schemas.ChatContentBlock {
	return schemas.ChatContentBlock{
		Type: schemas.ChatContentBlockTypeImage,
		ImageURLStruct: &schemas.ChatInputImage{
			URL: img.ToDataURL(),
		},
		CacheControl: cc,
	}
}

// ParseImage extracts an image from a string, map, or object across Anthropic, OpenAI, Codex, and Grok formats.
func ParseImage(v any) *ExtractedImage {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(trimmed, "data:image/") {
			semi := strings.Index(trimmed, ";")
			comma := strings.Index(trimmed, ",")
			if semi > 5 && comma > semi {
				mime := strings.ToLower(trimmed[5:semi])
				rawData := trimmed[comma+1:]
				cleanData, ok := CleanAndValidateBase64(rawData)
				if !ok {
					return nil
				}
				return &ExtractedImage{
					MediaType: mime,
					Data:      cleanData,
					URL:       fmt.Sprintf("data:%s;base64,%s", mime, cleanData),
				}
			}
			return nil
		}
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			return &ExtractedImage{URL: trimmed}
		}
	case map[string]any:
		// 1. Anthropic source format: {"source": {"type": "base64", "media_type": "...", "data": "..."}}
		if src, ok := val["source"].(map[string]any); ok {
			if img := parseImageFromSource(src); img != nil {
				return img
			}
		}

		// 2. image_url format: {"image_url": "..."} or {"image_url": {"url": "..."}}
		if imgURL, ok := val["image_url"]; ok {
			if str, ok := imgURL.(string); ok && str != "" {
				return ParseImage(str)
			}
			if m, ok := imgURL.(map[string]any); ok {
				if u := stringValue(m["url"]); u != "" {
					return ParseImage(u)
				}
			}
		}

		// 3. direct url: {"url": "..."}
		if u := stringValue(val["url"]); u != "" {
			return ParseImage(u)
		}

		// 4. file_data, data, image, image_data with mime/media type (Grok / Codex)
		data := stringValue(val["file_data"])
		if data == "" {
			data = stringValue(val["data"])
		}
		if data == "" {
			data = stringValue(val["image_data"])
		}
		if data == "" {
			data = stringValue(val["image"])
		}

		if data != "" {
			if strings.HasPrefix(data, "data:image/") {
				return ParseImage(data)
			}
			mime := stringValue(val["mime_type"])
			if mime == "" {
				mime = stringValue(val["media_type"])
			}
			if mime == "" {
				mime = stringValue(val["file_type"])
			}
			if mime == "" {
				mime = "image/png"
			}
			cleanData, ok := CleanAndValidateBase64(data)
			if !ok {
				return nil
			}
			return &ExtractedImage{
				MediaType: mime,
				Data:      cleanData,
				URL:       fmt.Sprintf("data:%s;base64,%s", mime, cleanData),
			}
		}
	}
	return nil
}

func parseImageFromSource(src map[string]any) *ExtractedImage {
	srcType := stringValue(src["type"])
	if srcType == "base64" || srcType == "" {
		mediaType := stringValue(src["media_type"])
		if mediaType == "" {
			mediaType = stringValue(src["mime_type"])
		}
		if mediaType == "" {
			mediaType = "image/png"
		}
		data := stringValue(src["data"])
		cleanData, ok := CleanAndValidateBase64(data)
		if !ok {
			return nil
		}
		return &ExtractedImage{
			MediaType: mediaType,
			Data:      cleanData,
			URL:       fmt.Sprintf("data:%s;base64,%s", mediaType, cleanData),
		}
	} else if srcType == "url" {
		u := stringValue(src["url"])
		if u != "" {
			return &ExtractedImage{URL: u}
		}
	}
	return nil
}

// ParseDocument extracts a document/PDF block from Anthropic, Grok, or Codex payload.
func ParseDocument(v any, cc *schemas.CacheControl) *schemas.ChatContentBlock {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}

	// 1. Anthropic source format
	if src, ok := m["source"].(map[string]any); ok {
		srcType := stringValue(src["type"])
		if srcType == "base64" || srcType == "text" || srcType == "" {
			data := stringValue(src["data"])
			data = strings.ReplaceAll(data, "\r", "")
			data = strings.ReplaceAll(data, "\n", "")
			mediaType := stringValue(src["media_type"])
			if mediaType == "" {
				mediaType = stringValue(src["mime_type"])
			}
			if mediaType == "" {
				mediaType = "application/pdf"
			}
			if data != "" {
				return &schemas.ChatContentBlock{
					Type: schemas.ChatContentBlockTypeFile,
					File: &schemas.ChatInputFile{
						FileData: &data,
						FileType: &mediaType,
					},
					CacheControl: cc,
				}
			}
		} else if srcType == "url" {
			u := stringValue(src["url"])
			if u != "" {
				return &schemas.ChatContentBlock{
					Type: schemas.ChatContentBlockTypeFile,
					File: &schemas.ChatInputFile{
						FileURL: &u,
					},
					CacheControl: cc,
				}
			}
		}
	}

	// 2. Grok/Codex input_file format: {"file_data": "...", "mime_type": "application/pdf"}
	data := stringValue(m["file_data"])
	if data == "" {
		data = stringValue(m["data"])
	}
	if data != "" {
		mime := stringValue(m["mime_type"])
		if mime == "" {
			mime = stringValue(m["media_type"])
		}
		if mime == "" {
			mime = "application/pdf"
		}
		data = strings.ReplaceAll(data, "\r", "")
		data = strings.ReplaceAll(data, "\n", "")
		return &schemas.ChatContentBlock{
			Type: schemas.ChatContentBlockTypeFile,
			File: &schemas.ChatInputFile{
				FileData: &data,
				FileType: &mime,
			},
			CacheControl: cc,
		}
	}

	// 3. file_url or url
	u := stringValue(m["file_url"])
	if u == "" {
		u = stringValue(m["url"])
	}
	if u != "" {
		return &schemas.ChatContentBlock{
			Type: schemas.ChatContentBlockTypeFile,
			File: &schemas.ChatInputFile{
				FileURL: &u,
			},
			CacheControl: cc,
		}
	}

	return nil
}

// ExtractToolImagesAndText extracts any images out of a tool output (which may be a plain string,
// a data URL, a JSON string, a map, or a list of parts). It returns a clean plain text / JSON
// representation (with massive image base64 stripped out) and the list of extracted images.
func ExtractToolImagesAndText(outputRaw any) (string, []*ExtractedImage) {
	if outputRaw == nil {
		return "{}", nil
	}

	switch val := outputRaw.(type) {
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return "{}", nil
		}

		// Direct data URL string
		if strings.HasPrefix(trimmed, "data:image/") {
			img := ParseImage(trimmed)
			if img != nil {
				return fmt.Sprintf("[image: %s]", img.MediaType), []*ExtractedImage{img}
			}
		}

		// JSON string: attempt to parse and recursively extract
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			var unmarshaled any
			if err := json.Unmarshal([]byte(trimmed), &unmarshaled); err == nil {
				cleanText, images := ExtractToolImagesAndText(unmarshaled)
				if len(images) > 0 {
					return cleanText, images
				}
				return trimmed, nil
			}
		}

		// Check for embedded data:image URLs inside markdown or text
		matches := ExtractDataURLMatches(trimmed)
		if len(matches) > 0 {
			images := make([]*ExtractedImage, 0, len(matches))
			clean := trimmed
			for _, match := range matches {
				img := ParseImage(match)
				if img != nil {
					images = append(images, img)
					clean = strings.Replace(clean, match, fmt.Sprintf("[image: %s]", img.MediaType), 1)
				}
			}
			if len(images) > 0 {
				return clean, images
			}
		}

		return trimmed, nil

	case map[string]any:
		return extractFromMap(val)

	case []any:
		return extractFromSlice(val)

	default:
		return fmt.Sprintf("%v", outputRaw), nil
	}
}

func extractFromMap(m map[string]any) (string, []*ExtractedImage) {
	// Check if this map is primarily an image container (like input_image, image_url, source)
	t := stringValue(m["type"])
	if t == "input_image" || t == "image" || t == "image_url" {
		if img := ParseImage(m); img != nil {
			return fmt.Sprintf("[image: %s]", img.MediaType), []*ExtractedImage{img}
		}
	}

	// Check if the map only has image keys: {"image": "data:image/..."} or {"image_url": "..."}
	if len(m) == 1 {
		if img := ParseImage(m); img != nil {
			return fmt.Sprintf("[image: %s]", img.MediaType), []*ExtractedImage{img}
		}
	}

	// General map: clone and scan all fields
	cleanMap := make(map[string]any, len(m))
	var images []*ExtractedImage

	for k, v := range m {
		switch fVal := v.(type) {
		case string:
			if strings.HasPrefix(strings.TrimSpace(fVal), "data:image/") {
				if img := ParseImage(fVal); img != nil {
					images = append(images, img)
					cleanMap[k] = fmt.Sprintf("[image: %s]", img.MediaType)
					continue
				}
			}
			// Embedded data url in string
			matches := ExtractDataURLMatches(fVal)
			if len(matches) > 0 {
				subClean := fVal
				for _, match := range matches {
					if img := ParseImage(match); img != nil {
						images = append(images, img)
						subClean = strings.Replace(subClean, match, fmt.Sprintf("[image: %s]", img.MediaType), 1)
					}
				}
				cleanMap[k] = subClean
				continue
			}
			cleanMap[k] = fVal

		case map[string]any:
			subText, subImgs := extractFromMap(fVal)
			if len(subImgs) > 0 {
				images = append(images, subImgs...)
				var subParsed any
				if json.Unmarshal([]byte(subText), &subParsed) == nil {
					cleanMap[k] = subParsed
				} else {
					cleanMap[k] = subText
				}
			} else {
				cleanMap[k] = fVal
			}

		case []any:
			subText, subImgs := extractFromSlice(fVal)
			if len(subImgs) > 0 {
				images = append(images, subImgs...)
				var subParsed any
				if json.Unmarshal([]byte(subText), &subParsed) == nil {
					cleanMap[k] = subParsed
				} else {
					cleanMap[k] = subText
				}
			} else {
				cleanMap[k] = fVal
			}

		default:
			cleanMap[k] = v
		}
	}

	if len(images) > 0 {
		b, err := json.Marshal(cleanMap)
		if err == nil {
			return string(b), images
		}
	}

	b, _ := json.Marshal(m)
	return string(b), images
}

func extractFromSlice(list []any) (string, []*ExtractedImage) {
	var textParts []string
	var images []*ExtractedImage

	for _, item := range list {
		switch elem := item.(type) {
		case string:
			clean, imgs := ExtractToolImagesAndText(elem)
			if len(imgs) > 0 {
				images = append(images, imgs...)
			}
			if clean != "" && clean != "{}" {
				textParts = append(textParts, clean)
			}
		case map[string]any:
			t := stringValue(elem["type"])
			if t == "input_image" || t == "image" || t == "image_url" {
				if img := ParseImage(elem); img != nil {
					images = append(images, img)
					continue
				}
			}
			clean, imgs := extractFromMap(elem)
			if len(imgs) > 0 {
				images = append(images, imgs...)
			}
			if clean != "" && clean != "{}" {
				// If it has text field, prefer the text field
				if txt := stringValue(elem["text"]); txt != "" {
					textParts = append(textParts, txt)
				} else {
					textParts = append(textParts, clean)
				}
			}
		default:
			if elem != nil {
				textParts = append(textParts, fmt.Sprintf("%v", elem))
			}
		}
	}

	plainText := strings.Join(textParts, "\n")
	if plainText == "" {
		if len(images) > 0 {
			plainText = fmt.Sprintf("[image: %s]", images[0].MediaType)
		} else {
			plainText = "{}"
		}
	}
	return plainText, images
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
