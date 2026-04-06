package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for countText
// ---------------------------------------------------------------------------

func TestCountText_SimplePhrase(t *testing.T) {
	result := countText("hello world")
	expect(t, "words", result.Words, 2)
	expect(t, "characters", result.Characters, 11)
	expect(t, "sentences", result.Sentences, 1) // no punctuation → counts as 1
	expect(t, "paragraphs", result.Paragraphs, 1)
}

func TestCountText_EmptyString(t *testing.T) {
	result := countText("")
	expect(t, "words", result.Words, 0)
	expect(t, "characters", result.Characters, 0)
	expect(t, "sentences", result.Sentences, 0)
	expect(t, "paragraphs", result.Paragraphs, 0)
}

func TestCountText_WhitespaceOnly(t *testing.T) {
	result := countText("   \t\n  ")
	expect(t, "words", result.Words, 0)
	expect(t, "sentences", result.Sentences, 0)
	expect(t, "paragraphs", result.Paragraphs, 0)
}

func TestCountText_SingleWord(t *testing.T) {
	result := countText("Go")
	expect(t, "words", result.Words, 1)
	expect(t, "characters", result.Characters, 2)
	expect(t, "sentences", result.Sentences, 1)
	expect(t, "paragraphs", result.Paragraphs, 1)
}

func TestCountText_MultipleSentences(t *testing.T) {
	result := countText("Hello world. How are you? I am fine!")
	expect(t, "words", result.Words, 8)
	expect(t, "sentences", result.Sentences, 3)
	expect(t, "paragraphs", result.Paragraphs, 1)
}

func TestCountText_MultipleParagraphs(t *testing.T) {
	text := "First paragraph here.\n\nSecond paragraph here.\n\nThird one."
	result := countText(text)
	expect(t, "words", result.Words, 8)
	expect(t, "paragraphs", result.Paragraphs, 3)
	expect(t, "sentences", result.Sentences, 3)
}

func TestCountText_ConsecutiveBlankLines(t *testing.T) {
	text := "One.\n\n\n\nTwo."
	result := countText(text)
	expect(t, "paragraphs", result.Paragraphs, 2)
	expect(t, "sentences", result.Sentences, 2)
}

func TestCountText_TrailingNewlines(t *testing.T) {
	text := "Hello world.\n\n"
	result := countText(text)
	expect(t, "words", result.Words, 2)
	expect(t, "paragraphs", result.Paragraphs, 1)
	expect(t, "sentences", result.Sentences, 1)
}

func TestCountText_Unicode(t *testing.T) {
	// "Hello" in Japanese (5 chars) + space + emoji (1 char) = 7 runes
	text := "こんにちは 🌍"
	result := countText(text)
	expect(t, "words", result.Words, 2)
	expect(t, "characters", result.Characters, 7)
	expect(t, "paragraphs", result.Paragraphs, 1)
}

func TestCountText_SentenceEndingAtEOF(t *testing.T) {
	result := countText("Done.")
	expect(t, "sentences", result.Sentences, 1)
}

func TestCountText_EllipsisMidSentence(t *testing.T) {
	// "3.14" — period NOT followed by space/EOF, should not count as sentence end.
	result := countText("The value is 3.14 exactly")
	expect(t, "sentences", result.Sentences, 1)
}

func TestCountText_QuotedSentence(t *testing.T) {
	result := countText(`She said "wow!" and left.`)
	// "wow!" period followed by quote → sentence, "left." at EOF → sentence
	expect(t, "sentences", result.Sentences, 2)
}

func TestCountText_MultilineWithSingleNewlines(t *testing.T) {
	// Single newlines do NOT create new paragraphs — only blank lines do.
	text := "line one\nline two\nline three"
	result := countText(text)
	expect(t, "paragraphs", result.Paragraphs, 1)
	expect(t, "words", result.Words, 6)
}

func TestCountText_LongerPassage(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs!\n\n" +
		"How vexingly quick daft zebras jump?"
	result := countText(text)
	expect(t, "words", result.Words, 23)
	expect(t, "sentences", result.Sentences, 3)
	expect(t, "paragraphs", result.Paragraphs, 2)
}

// ---------------------------------------------------------------------------
// HTTP handler integration tests
// ---------------------------------------------------------------------------

func TestHandleWordCount_Success(t *testing.T) {
	body := mustJSON(t, providerRequest{
		ToolName: "word_count",
		Input:    map[string]any{"text": "Hello world. Goodbye world."},
	})

	rr := postJSON(t, "/", body)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result wordCountResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expect(t, "words", result.Words, 4)
	expect(t, "sentences", result.Sentences, 2)
	expect(t, "characters", result.Characters, 27)
	expect(t, "paragraphs", result.Paragraphs, 1)
}

func TestHandleWordCount_EmptyText(t *testing.T) {
	body := mustJSON(t, providerRequest{
		ToolName: "word_count",
		Input:    map[string]any{"text": ""},
	})

	rr := postJSON(t, "/", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result wordCountResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expect(t, "words", result.Words, 0)
	expect(t, "sentences", result.Sentences, 0)
}

func TestHandleWordCount_MissingTextField(t *testing.T) {
	body := mustJSON(t, providerRequest{
		ToolName: "word_count",
		Input:    map[string]any{"wrong_field": "oops"},
	})

	rr := postJSON(t, "/", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var errResp map[string]string
	json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestHandleWordCount_TextNotString(t *testing.T) {
	body := mustJSON(t, providerRequest{
		ToolName: "word_count",
		Input:    map[string]any{"text": 12345},
	})

	rr := postJSON(t, "/", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleWordCount_InvalidJSON(t *testing.T) {
	rr := postJSON(t, "/", []byte(`{not valid json`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleWordCount_NilInput(t *testing.T) {
	body := mustJSON(t, map[string]any{
		"tool_name": "word_count",
		"input":     nil,
	})

	rr := postJSON(t, "/", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
	if resp["service"] != "toolshed-wordcount" {
		t.Errorf("expected service=toolshed-wordcount, got %q", resp["service"])
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func expect(t *testing.T, field string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %d, want %d", field, got, want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	return data
}

func postJSON(t *testing.T, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleWordCount(rr, req)
	return rr
}
