// Package summarizer provides a backend-agnostic text summarization interface.
// Backends: "local" (FoundationModels via mp-summarize), "kagi" (Kagi API).
// Select via SUMMARIZE_BACKEND env var; defaults to "local".
package summarizer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Summarizer summarizes text to a short string.
type Summarizer interface {
	Summarize(text string) (string, error)
	Close() error
}

// ── Local (FoundationModels via mp-summarize subprocess) ─────────────────────

type localSummarizer struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
}

type sumInput  struct{ Text string `json:"text"` }
type sumOutput struct {
	Summary string `json:"summary"`
	Error   string `json:"error"`
}

// NewLocal starts the mp-summarize subprocess at binPath.
// Requires macOS 26+ with FoundationModels available.
func NewLocal(binPath string) (Summarizer, error) {
	cmd := exec.Command(binPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("summarizer stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("summarizer stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mp-summarize: %w", err)
	}
	return &localSummarizer{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdoutPipe),
	}, nil
}

func (s *localSummarizer) Summarize(text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	line, err := json.Marshal(sumInput{Text: text})
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(s.stdin, "%s\n", line); err != nil {
		return "", fmt.Errorf("write to mp-summarize: %w", err)
	}

	if !s.stdout.Scan() {
		if err := s.stdout.Err(); err != nil {
			return "", fmt.Errorf("read from mp-summarize: %w", err)
		}
		return "", fmt.Errorf("mp-summarize closed unexpectedly")
	}

	var out sumOutput
	if err := json.Unmarshal(s.stdout.Bytes(), &out); err != nil {
		return "", fmt.Errorf("parse mp-summarize output: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("mp-summarize: %s", out.Error)
	}
	return out.Summary, nil
}

func (s *localSummarizer) Close() error {
	s.stdin.Close()
	return s.cmd.Wait()
}

// TextForEmbedding returns the text to embed/summarize for a given record,
// combining title and body with a separator.
func TextForEmbedding(title, body string) string {
	if body == "" {
		return title
	}
	if len(body) > 500 {
		body = body[:500]
	}
	if title == "" {
		return body
	}
	return title + ". " + body
}
