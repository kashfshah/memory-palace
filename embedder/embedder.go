// Package embedder provides a backend-agnostic text embedding interface.
// Currently supports NLEmbedding via the mp-embed Swift subprocess.
// Additional backends (OpenAI, Ollama, etc.) can be added by implementing
// the Embedder interface and selecting via EMBED_BACKEND env var.
package embedder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
)

const Dimensions = 512 // NLEmbedding sentence embedding for English

// Embedder embeds text into a float32 vector.
type Embedder interface {
	Embed(text string) ([]float32, error)
	Close() error
}

// subprocessEmbedder keeps mp-embed alive and pipes text through it.
type subprocessEmbedder struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
}

type embedInput  struct{ Text string `json:"text"` }
type embedOutput struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error"`
}

// New starts the mp-embed subprocess at binPath and returns an Embedder.
// Call Close() when done to terminate the subprocess.
func New(binPath string) (Embedder, error) {
	cmd := exec.Command(binPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("embedder stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("embedder stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mp-embed: %w", err)
	}
	return &subprocessEmbedder{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdoutPipe),
	}, nil
}

func (e *subprocessEmbedder) Embed(text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	line, err := json.Marshal(embedInput{Text: text})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(e.stdin, "%s\n", line); err != nil {
		return nil, fmt.Errorf("write to mp-embed: %w", err)
	}

	if !e.stdout.Scan() {
		if err := e.stdout.Err(); err != nil {
			return nil, fmt.Errorf("read from mp-embed: %w", err)
		}
		return nil, fmt.Errorf("mp-embed closed unexpectedly")
	}

	var out embedOutput
	if err := json.Unmarshal(e.stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("parse mp-embed output: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("mp-embed: %s", out.Error)
	}
	return out.Embedding, nil
}

func (e *subprocessEmbedder) Close() error {
	e.stdin.Close()
	return e.cmd.Wait()
}

// Cosine returns the cosine similarity between two equal-length vectors.
// Returns 0 if either vector has zero magnitude.
func Cosine(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
