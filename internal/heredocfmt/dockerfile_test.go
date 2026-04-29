package heredocfmt

import "testing"

func TestShellFromHeredocShebangIgnoresIndentedShebangComment(t *testing.T) {
	t.Parallel()

	if got, ok := shellFromHeredocShebang("  #!/usr/bin/env bash\necho hi\n"); ok {
		t.Fatalf("shellFromHeredocShebang() = %q, true; want no shebang", got)
	}
}
