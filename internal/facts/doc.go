// Package facts provides a per-file derived-analysis layer for Dockerfiles.
//
// Facts are computed once per linted file, cached, and shared by all rules.
// The layer is intentionally generic:
// it captures effective stage state (shell, env, workdir) and projects that
// state onto RUN instructions as typed facts that downstream rules can reuse
// without reparsing scripts or rebuilding heuristic context themselves.
//
// Rules consume facts read-only; they do not mutate or author facts.
package facts
