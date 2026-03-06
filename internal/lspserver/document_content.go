package lspserver

import (
	"log"
	"os"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/fileval"
)

func (s *Server) readValidatedFileContent(filePath string) ([]byte, bool) {
	cfg := s.resolveConfig(filePath)
	maxSize := config.Default().FileValidation.MaxFileSize
	if cfg != nil {
		maxSize = cfg.FileValidation.MaxFileSize
	}

	if err := fileval.ValidateFile(filePath, maxSize); err != nil {
		log.Printf("lsp: file validation failed for %s: %v", filePath, err)
		return nil, false
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}
	return content, true
}
