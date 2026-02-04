package shell

import (
	"slices"
	"testing"
)

func TestSplitSimpleCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantOK  bool
		wantArg []string
	}{
		{name: "simple", cmd: "echo hi", wantOK: true, wantArg: []string{"echo", "hi"}},
		{name: "single-quoted", cmd: "echo 'hello world'", wantOK: true, wantArg: []string{"echo", "hello world"}},
		{name: "double-quoted-literal", cmd: `echo "hello world"`, wantOK: true, wantArg: []string{"echo", "hello world"}},
		{name: "mixed-quoted", cmd: `echo foo"bar"`, wantOK: true, wantArg: []string{"echo", "foobar"}},
		{name: "empty-arg", cmd: "echo ''", wantOK: true, wantArg: []string{"echo", ""}},

		{name: "empty", cmd: "", wantOK: false},
		{name: "parse-error", cmd: "echo 'unterminated", wantOK: false},
		{name: "multi-stmt", cmd: "echo hi; echo there", wantOK: false},
		{name: "pipeline", cmd: "echo hi | cat", wantOK: false},
		{name: "redir", cmd: "echo hi > out", wantOK: false},
		{name: "background", cmd: "echo hi &", wantOK: false},
		{name: "negated", cmd: "! echo hi", wantOK: false},
		{name: "assign", cmd: "FOO=bar echo hi", wantOK: false},
		{name: "param-expansion", cmd: "echo $HOME", wantOK: false},
		{name: "param-expansion-double-quoted", cmd: `echo "$HOME"`, wantOK: false},
		{name: "unquoted-glob", cmd: "echo *.txt", wantOK: false},
		{name: "quoted-glob", cmd: `echo "*.txt"`, wantOK: true, wantArg: []string{"echo", "*.txt"}},

		{name: "tilde", cmd: "echo ~/bin", wantOK: false},
		{name: "tilde-user", cmd: "echo ~root", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SplitSimpleCommand(tt.cmd, VariantBash)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (args=%v)", ok, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if !slices.Equal(got, tt.wantArg) {
				t.Fatalf("args = %v, want %v", got, tt.wantArg)
			}
		})
	}
}
