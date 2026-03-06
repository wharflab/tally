package lspserver

import (
	"log"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/fileval"
)

func (s *Server) readValidatedFileContent(filePath string) ([]byte, bool) {
	cfg := s.resolveConfig(filePath)
	maxSize := config.Default().FileValidation.MaxFileSize
	if cfg != nil {
		maxSize = cfg.FileValidation.MaxFileSize
	}

	content, err := fileval.ReadValidatedFile(filePath, maxSize)
	if err != nil {
		log.Printf("lsp: file validation failed for %s: %v", filePath, err)
		return nil, false
	}
	return content, true
}
