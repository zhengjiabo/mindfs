package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"mindfs/server/internal/agent"
	"mindfs/server/internal/apperr"
)

const codexConfigModelMaxLen = 200

var (
	codexConfigModelMu     sync.Mutex
	errCodexConfigModelReq = errors.New("invalid codex config model")
)

type codexConfigModelRequest struct {
	Model string `json:"model"`
}

func (h *HTTPHandler) handleCodexConfigModelSet(w http.ResponseWriter, r *http.Request) {
	var req codexConfigModelRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxUploadRequestBytes)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid request body"))
		return
	}
	previous, model, changed, err := setCodexConfigModel(req.Model)
	if err != nil {
		if errors.Is(err, errCodexConfigModelReq) {
			message := strings.TrimSpace(strings.TrimPrefix(err.Error(), errCodexConfigModelReq.Error()+":"))
			if message == "" {
				message = err.Error()
			}
			respondError(w, http.StatusBadRequest, errInvalidRequest(message))
			return
		}
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	triggerAgentConfigSwitchProbe(h.AppContext, "codex")
	log.Printf("[agent-config] codex_model.set previous=%q model=%q changed=%t", previous, model, changed)
	respondJSON(w, http.StatusOK, map[string]any{
		"agent":          "codex",
		"model":          model,
		"previous_model": previous,
		"changed":        changed,
	})
}

func setCodexConfigModel(rawModel string) (previous string, model string, changed bool, err error) {
	model, err = normalizeCodexConfigModel(rawModel)
	if err != nil {
		return "", "", false, err
	}

	codexConfigModelMu.Lock()
	defer codexConfigModelMu.Unlock()

	home := strings.TrimSpace(agent.CodexHomeDir())
	if home == "" {
		return "", "", false, fmt.Errorf("codex home unavailable")
	}
	configPath := filepath.Join(home, "config.toml")

	content := ""
	perm := os.FileMode(0o600)
	if raw, readErr := os.ReadFile(configPath); readErr == nil {
		content = string(raw)
		if info, statErr := os.Stat(configPath); statErr == nil {
			perm = info.Mode().Perm()
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", "", false, apperr.Wrap("read", configPath, readErr)
	}

	next, previous, changed, err := patchTopLevelTOMLStringKey(content, "model", model)
	if err != nil {
		return "", "", false, err
	}
	if !changed {
		return previous, model, false, nil
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", "", false, apperr.Wrap("mkdir", home, err)
	}
	if err := os.WriteFile(configPath, []byte(next), perm); err != nil {
		return "", "", false, apperr.Wrap("write", configPath, err)
	}
	return previous, model, true, nil
}

func normalizeCodexConfigModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return "", fmt.Errorf("%w: model required", errCodexConfigModelReq)
	}
	if strings.ContainsAny(model, "\n\r\x00") {
		return "", fmt.Errorf("%w: model must not contain newlines or NUL", errCodexConfigModelReq)
	}
	if utf8.RuneCountInString(model) > codexConfigModelMaxLen {
		return "", fmt.Errorf("%w: model too long", errCodexConfigModelReq)
	}
	return model, nil
}

// patchTopLevelTOMLStringKey sets a top-level string key in a TOML document
// without re-serializing the full file. Only the preamble before the first
// [section] is considered. Section-local keys are left untouched.
func patchTopLevelTOMLStringKey(content, key, value string) (next string, previous string, changed bool, err error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, errors.New("toml key required")
	}
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", "", false, errors.New("toml value must not contain newlines or NUL")
	}

	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	sectionIdx := -1
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionIdx = i
			break
		}
	}

	topEnd := len(lines)
	if sectionIdx >= 0 {
		topEnd = sectionIdx
	}

	quoted := quoteTOMLString(value)
	keyPattern := regexp.MustCompile(
		`(?i)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*("[^"]*"|'[^']*'|[^#\n]+?)[ \t]*(#.*)?$`,
	)

	replaced := false
	for i := 0; i < topEnd; i++ {
		if !keyPattern.MatchString(lines[i]) {
			continue
		}
		previous = readTopLevelTOMLStringKeyFromLine(lines[i], key)
		if previous == value {
			return content, previous, false, nil
		}
		lines[i] = key + " = " + quoted
		replaced = true
		break
	}

	if !replaced {
		// No top-level key: insert before first section (or append).
		previous = ""
		insertLine := key + " = " + quoted
		if sectionIdx >= 0 {
			insertAt := sectionIdx
			// Prefer a blank line before section if the previous line is non-empty content.
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:insertAt]...)
			// Drop trailing empty lines in top just before insert to avoid runaway blanks,
			// but keep a single separating blank if section follows.
			for len(newLines) > 0 && strings.TrimSpace(newLines[len(newLines)-1]) == "" {
				newLines = newLines[:len(newLines)-1]
			}
			if len(newLines) > 0 {
				newLines = append(newLines, insertLine, "")
			} else {
				newLines = append(newLines, insertLine, "")
			}
			newLines = append(newLines, lines[insertAt:]...)
			lines = newLines
		} else {
			if len(lines) == 1 && lines[0] == "" {
				lines = []string{insertLine}
			} else {
				if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
					lines[len(lines)-1] = insertLine
				} else {
					lines = append(lines, insertLine)
				}
			}
		}
	}

	joined := strings.Join(lines, "\n")
	// Preserve trailing newline if original had one or file non-empty.
	if content == "" {
		next = joined + "\n"
	} else if strings.HasSuffix(normalized, "\n") || strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\r\n") {
		if !strings.HasSuffix(joined, "\n") {
			joined += "\n"
		}
		next = joined
	} else {
		next = joined
	}
	if newline == "\r\n" {
		next = strings.ReplaceAll(next, "\n", "\r\n")
	}
	if next == content {
		return content, previous, false, nil
	}
	return next, previous, true, nil
}

func readTopLevelTOMLStringKeyFromLine(line, key string) string {
	keyPattern := regexp.MustCompile(
		`(?i)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*("[^"]*"|'[^']*'|[^#\n]+?)[ \t]*(#.*)?$`,
	)
	match := keyPattern.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	raw := strings.TrimSpace(match[1])
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func quoteTOMLString(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
	)
	return `"` + replacer.Replace(value) + `"`
}
