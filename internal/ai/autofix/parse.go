package autofix

import (
	"errors"
	"strings"

	"github.com/wharflab/tally/internal/ai/autofixdata"
)

func parseAgentPatchResponse(text string) (string, bool, error) {
	return parseAgentFencedResponse(text, "diff", "diff patch")
}

func parseAgentDockerfileResponse(text string) (string, bool, error) {
	return parseAgentFencedResponse(text, "Dockerfile", "Dockerfile")
}

func parseAgentFencedResponse(text, infoString, label string) (string, bool, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false, errors.New("empty agent output")
	}
	if trimmed == "NO_CHANGE" {
		return "", true, nil
	}

	trimmed = autofixdata.NormalizeLF(trimmed)
	opening := "```" + infoString + "\n"
	if !strings.HasPrefix(trimmed, opening) {
		return "", false, errors.New("output must be a single ```" + infoString + " fenced block or NO_CHANGE")
	}
	if !strings.HasSuffix(trimmed, "\n```") {
		return "", false, errors.New("output must end with a closing ``` fence")
	}
	body := trimmed[len(opening) : len(trimmed)-len("```")]
	if strings.TrimSpace(body) == "" {
		return "", false, errors.New("empty " + label + " code block")
	}
	return body, false, nil
}
