package hadolint

// DL3061: Invalid instruction order - Dockerfile must begin with FROM, ARG, or comment.
//
// A Dockerfile must start with either a FROM instruction (to specify the base image),
// an ARG instruction (to define build arguments that can be used in FROM), or comments.
// Any other instruction appearing before FROM (except ARG) is invalid.
//
// IMPLEMENTATION: This rule is detected during semantic analysis in
// internal/semantic/builder.go by examining the raw AST. The builder iterates
// through top-level instructions and reports a violation if it encounters any
// instruction other than FROM or ARG before the first FROM instruction.
//
// See: https://github.com/hadolint/hadolint/wiki/DL3061

const (
	DL3061Code    = "hadolint/DL3061"
	DL3061Message = "Dockerfile must begin with FROM or ARG"
	DL3061DocURL  = "https://github.com/hadolint/hadolint/wiki/DL3061"
)
