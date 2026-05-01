package heredocfmt

import "testing"

func TestShellFromHeredocShebangIgnoresIndentedShebangComment(t *testing.T) {
	t.Parallel()

	if got, ok := shellFromHeredocShebang("  #!/usr/bin/env bash\necho hi\n"); ok {
		t.Fatalf("shellFromHeredocShebang() = %q, true; want no shebang", got)
	}
}

func TestIsPowerShellTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target string
		want   bool
	}{
		{"/opt/app/install.ps1", true},
		{"/opt/app/MyModule.psm1", true},
		{"/opt/app/MyModule.psd1", true},
		{"/opt/app/config.json", false},
		{"/opt/app/script.sh", false},
	}
	for _, tt := range tests {
		if got := IsPowerShellTarget(tt.target); got != tt.want {
			t.Fatalf("IsPowerShellTarget(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}
