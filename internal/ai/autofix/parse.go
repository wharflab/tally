package autofix

import (
	"errors"
	"strings"
)

func parseAgentResponse(text string) (string, bool, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false, errors.New("empty agent output")
	}
	if trimmed == "NO_CHANGE" {
		return "", true, nil
	}

	trimmed = normalizeLF(trimmed)
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return "", false, errors.New("expected a Dockerfile code block or NO_CHANGE")
	}
	if strings.TrimSpace(lines[0]) != "```Dockerfile" {
		return "", false, errors.New("output must be a single ```Dockerfile fenced block or NO_CHANGE")
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", false, errors.New("output must end with a closing ``` fence")
	}

	body := strings.Join(lines[1:len(lines)-1], "\n")
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return "", false, errors.New("empty Dockerfile code block")
	}
	return body, false, nil
}
