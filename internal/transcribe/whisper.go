package transcribe

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ohmyvoice/internal/config"
)

type Engine struct {
	cliPath   string
	modelPath string
	language  string
	threads   int
}

func NewEngine(baseDir string, cfg *config.Config) (*Engine, error) {
	cliPath := filepath.Join(baseDir, "bin", "whisper-cli.exe")
	if _, err := os.Stat(cliPath); err != nil {
		return nil, fmt.Errorf("whisper-cli.exe not found at %s: %w", cliPath, err)
	}

	modelFile := cfg.Model
	if modelFile == "" {
		modelFile = "ggml-small.bin"
	}
	modelPath := filepath.Join(baseDir, "models", modelFile)
	if _, err := os.Stat(modelPath); err != nil {
		modelPath = filepath.Join(baseDir, "models", "ggml-base.bin")
		if _, err := os.Stat(modelPath); err != nil {
			return nil, fmt.Errorf("no model found in %s", filepath.Join(baseDir, "models"))
		}
	}

	lang := cfg.Language
	if lang == "" {
		lang = "ru"
	}

	threads := cfg.Threads
	if threads <= 0 {
		threads = 6
	}

	return &Engine{
		cliPath:   cliPath,
		modelPath: modelPath,
		language:  lang,
		threads:   threads,
	}, nil
}

func (e *Engine) Transcribe(wavPath string) (string, error) {
	prompt := "Привет! Разговорная речь и диктовка со знаками препинания: программирование, разработка, дупликация, код, функция, Windows, GPU."
	if e.language == "en" {
		prompt = "Hello! Speech-to-text dictation with proper punctuation and technical terminology."
	}

	args := []string{
		"-m", e.modelPath,
		wavPath,
		"-l", e.language,
		"-dev", "0",
		"--prompt", prompt,
		"--no-timestamps",
		"--no-prints",
		"-t", fmt.Sprintf("%d", e.threads),
	}

	cmd := exec.Command(e.cliPath, args...)
	cmd.Dir = filepath.Dir(e.cliPath)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("whisper failed: %w, stderr: %s", err, stderr.String())
	}

	raw := stdout.String()
	cleaned := CleanTranscript(raw)
	return cleaned, nil
}

// CleanTranscript removes hallucinated tags, excess whitespace, and empty text
func CleanTranscript(text string) string {
	// Remove common whisper hallucination tags like [музыка], (звуки), etc.
	tagRegex := regexp.MustCompile(`\[.*?\]|\(.*?\)|<.*?>`)
	cleaned := tagRegex.ReplaceAllString(text, "")

	// Filter common subtitle hallucination patterns
	subRegex := regexp.MustCompile(`(?i)(редактор субтитров|корректор|переводчик субтитров|субтитры сделал).*?$`)
	cleaned = subRegex.ReplaceAllString(cleaned, "")

	// Normalize spaces and newlines
	cleaned = strings.ReplaceAll(cleaned, "\r\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\t", " ")

	fields := strings.Fields(cleaned)
	result := strings.Join(fields, " ")
	result = strings.TrimSpace(result)

	// Filter out if it's only punctuation
	punctOnly := regexp.MustCompile(`^[\.,!?\-\s_]+$`)
	if punctOnly.MatchString(result) {
		return ""
	}

	return result
}
