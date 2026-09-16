package media

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseImage(t *testing.T) {
	// 1. Data URL
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	img := ParseImage(dataURL)
	if img == nil {
		t.Fatalf("ParseImage failed for data URL")
	}
	if img.MediaType != "image/png" {
		t.Errorf("MediaType = %s, want image/png", img.MediaType)
	}
	if img.ToDataURL() != dataURL {
		t.Errorf("ToDataURL mismatch")
	}

	// 2. OpenAI image_url struct
	openAIImg := map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": dataURL,
		},
	}
	img2 := ParseImage(openAIImg)
	if img2 == nil || img2.MediaType != "image/png" {
		t.Errorf("ParseImage failed for OpenAI image_url: %+v", img2)
	}

	// 3. Anthropic source
	anthropicImg := map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/jpeg",
			"data":       "/9j/4AAQSkZJRg==",
		},
	}
	img3 := ParseImage(anthropicImg)
	if img3 == nil || img3.MediaType != "image/jpeg" || img3.Data != "/9j/4AAQSkZJRg==" {
		t.Errorf("ParseImage failed for Anthropic image: %+v", img3)
	}

	// 4. Grok file_data
	grokImg := map[string]any{
		"type":      "input_image",
		"file_data": "iVBORw0KGgo==",
		"mime_type": "image/webp",
	}
	img4 := ParseImage(grokImg)
	if img4 == nil || img4.MediaType != "image/webp" {
		t.Errorf("ParseImage failed for Grok image: %+v", img4)
	}
}

func TestExtractToolImagesAndText_DataURL(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	text, imgs := ExtractToolImagesAndText(dataURL)
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if text != "[image: image/png]" {
		t.Errorf("Text = %q, want [image: image/png]", text)
	}
}

func TestExtractToolImagesAndText_JSONWithImageAndMetadata(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	rawJSON := map[string]any{
		"status": "success",
		"path":   "/tmp/screenshot.png",
		"image":  dataURL,
	}
	rawBytes, _ := json.Marshal(rawJSON)

	text, imgs := ExtractToolImagesAndText(string(rawBytes))
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("Expected JSON output, got error: %v, text: %s", err, text)
	}
	if res["status"] != "success" || res["path"] != "/tmp/screenshot.png" {
		t.Errorf("Metadata preserved failed: %+v", res)
	}
	if res["image"] != "[image: image/png]" {
		t.Errorf("Image placeholder mismatch: %v", res["image"])
	}
}

func TestExtractToolImagesAndText_EmbeddedMarkdown(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	input := "Here is the chart: ![" + "plot" + "](" + dataURL + ")\nDone."

	text, imgs := ExtractToolImagesAndText(input)
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image, got %d", len(imgs))
	}
	if text == input {
		t.Errorf("Expected data URL in text to be replaced by placeholder")
	}
}

func TestCleanAndValidateBase64(t *testing.T) {
	// 1. Valid single-line base64
	valid := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	clean, ok := CleanAndValidateBase64(valid)
	if !ok || clean != valid {
		t.Errorf("Valid base64 failed validation: ok=%v, clean=%s", ok, clean)
	}

	// 2. Multiline payload with newlines and indentation (typical of RFC 2045 or terminal outputs)
	multiline := "iVBOR\n  w0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk\r\n  +A8AAQUBAScY42YAAAAASUVORK5CYII="
	cleanMulti, ok := CleanAndValidateBase64(multiline)
	if !ok || cleanMulti != valid {
		t.Errorf("Multiline base64 failed cleaning: ok=%v, got=%s", ok, cleanMulti)
	}

	// 3. Unpadded base64 (auto-repaired)
	unpadded := "iVBORw0KGgo" // 11 characters, 8 bytes
	cleanUnpadded, ok := CleanAndValidateBase64(unpadded)
	if !ok || cleanUnpadded != "iVBORw0KGgo=" {
		t.Errorf("Unpadded base64 failed: ok=%v, got=%s, want iVBORw0KGgo=", ok, cleanUnpadded)
	}

	// 4. Truncated fragment (e.g. "iVBOR" - exactly 5 chars, length % 4 == 1)
	// Must be rejected to protect upstream LLM providers (like Gemini) from 400 crashes!
	_, ok = CleanAndValidateBase64("iVBOR")
	if ok {
		t.Errorf("Expected 'iVBOR' to be rejected, but it passed validation!")
	}

	// 5. Corrupted base64 with ellipsis (e.g. "iVBORw0KGgo...")
	_, ok = CleanAndValidateBase64("iVBORw0KGgo...")
	if ok {
		t.Errorf("Expected ellipsis base64 to be rejected, but it passed validation!")
	}

	// 6. Empty string
	_, ok = CleanAndValidateBase64("")
	if ok {
		t.Errorf("Expected empty string to be rejected")
	}
}

func TestExtractToolImagesAndText_MultilineDataURL(t *testing.T) {
	multilineURL := "data:image/png;base64,iVBOR\n  w0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk\r\n  +A8AAQUBAScY42YAAAAASUVORK5CYII="
	input := "Screenshot result:\n" + multilineURL + "\nProcessing complete."

	cleanText, imgs := ExtractToolImagesAndText(input)
	if len(imgs) != 1 {
		t.Fatalf("Expected 1 image extracted from multiline data URL, got %d", len(imgs))
	}
	if imgs[0].MediaType != "image/png" {
		t.Errorf("MediaType = %s, want image/png", imgs[0].MediaType)
	}
	expectedValidData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	if imgs[0].Data != expectedValidData {
		t.Errorf("Extracted data not normalized: got %s, want %s", imgs[0].Data, expectedValidData)
	}
	if !strings.Contains(cleanText, "[image: image/png]") {
		t.Errorf("Expected clean placeholder in text, got: %s", cleanText)
	}
	if strings.Contains(cleanText, "iVBOR") {
		t.Errorf("Clean text still contains raw base64 data: %s", cleanText)
	}
}

func TestExtractToolImagesAndText_CorruptedDataSafelyIgnored(t *testing.T) {
	// A mock tool output with a truncated base64 placeholder
	corrupted := "Error: partial data:image/png;base64,iVBOR failed"
	cleanText, imgs := ExtractToolImagesAndText(corrupted)
	if len(imgs) != 0 {
		t.Errorf("Expected 0 images for corrupted data, got %d", len(imgs))
	}
	if cleanText != corrupted {
		t.Errorf("CleanText should remain unmolested when no valid images are found")
	}
}
