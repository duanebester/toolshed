// Word Count tool — a simple REST provider for the ToolShed SSH server.
//
// Accepts the standard ToolShed provider payload:
//
//	POST /
//	{ "tool_name": "word_count", "input": { "text": "..." } }
//
// Returns:
//
//	{ "words": 5, "characters": 23, "sentences": 1, "paragraphs": 1 }
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

type providerRequest struct {
	ToolName string         `json:"tool_name"`
	Input    map[string]any `json:"input"`
}

type wordCountResult struct {
	Words      int `json:"words"`
	Characters int `json:"characters"`
	Sentences  int `json:"sentences"`
	Paragraphs int `json:"paragraphs"`
}

func main() {
	port := envOr("PORT", "9090")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", handleWordCount)
	mux.HandleFunc("GET /healthz", handleHealth)

	log.Printf("wordcount tool listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleWordCount(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req providerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	textRaw, ok := req.Input["text"]
	if !ok {
		writeError(w, http.StatusBadRequest, "missing required input field: text")
		return
	}

	text, ok := textRaw.(string)
	if !ok {
		writeError(w, http.StatusBadRequest, "input field 'text' must be a string, got %T", textRaw)
		return
	}

	result := countText(text)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "toolshed-wordcount",
	})
}

// countText computes word, character, sentence, and paragraph counts.
func countText(text string) wordCountResult {
	if strings.TrimSpace(text) == "" {
		return wordCountResult{}
	}

	// Words: split on whitespace.
	words := len(strings.Fields(text))

	// Characters: count all runes (including spaces/punctuation).
	characters := utf8.RuneCountInString(text)

	// Sentences: count sentence-ending punctuation (.!?) that is followed
	// by whitespace or end-of-string.  This is intentionally simple.
	sentences := 0
	runes := []rune(text)
	for i, ch := range runes {
		if ch == '.' || ch == '!' || ch == '?' {
			// Only count if next char is whitespace, EOF, or a quote/paren.
			if i == len(runes)-1 {
				sentences++
			} else {
				next := runes[i+1]
				if unicode.IsSpace(next) || next == '"' || next == '\'' || next == ')' || next == ']' {
					sentences++
				}
			}
		}
	}
	// If the text has words but no detected sentence endings, count it as one sentence.
	if sentences == 0 && words > 0 {
		sentences = 1
	}

	// Paragraphs: groups of text separated by one or more blank lines.
	paragraphs := 0
	inParagraph := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if !inParagraph {
				paragraphs++
				inParagraph = true
			}
		} else {
			inParagraph = false
		}
	}

	return wordCountResult{
		Words:      words,
		Characters: characters,
		Sentences:  sentences,
		Paragraphs: paragraphs,
	}
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("ERROR %d: %s", status, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
