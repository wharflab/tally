package php

import (
	"testing"

	"github.com/wharflab/tally/internal/shell"
)

func TestStageLooksLikeDev(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stage string
		want  bool
	}{
		{name: "plain dev", stage: "dev", want: true},
		{name: "hyphenated token", stage: "builder-dev", want: true},
		{name: "underscored token", stage: "php_test", want: true},
		{name: "debug token", stage: "runtime.debug", want: true},
		{name: "non-dev word", stage: "device", want: false},
		{name: "empty", stage: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stageLooksLikeDev(tt.stage); got != tt.want {
				t.Errorf("stageLooksLikeDev(%q) = %v, want %v", tt.stage, got, tt.want)
			}
		})
	}
}

func TestPHPExtensionReferencesXdebug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{
			name: "docker-php-ext-install xdebug",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-install", Subcommand: "xdebug", Args: []string{"xdebug"}},
			want: true,
		},
		{
			name: "docker-php-ext-enable xdebug",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-enable", Subcommand: "xdebug", Args: []string{"xdebug"}},
			want: true,
		},
		{
			name: "docker-php-ext-install xdebug not first arg",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-install", Subcommand: "gd", Args: []string{"gd", "xdebug"}},
			want: true,
		},
		{
			name: "pecl install xdebug",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "install", Args: []string{"install", "xdebug"}},
			want: true,
		},
		{
			name: "pecl install versioned xdebug",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "install", Args: []string{"install", "xdebug-3.4.0"}},
			want: true,
		},
		{
			name: "pecl uninstall xdebug ignored",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "uninstall", Args: []string{"uninstall", "xdebug"}},
			want: false,
		},
		{
			name: "docker-php-ext-install gd only",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-install", Subcommand: "gd", Args: []string{"gd"}},
			want: false,
		},
		{
			name: "pecl install redis",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "install", Args: []string{"install", "redis"}},
			want: false,
		},
		{
			name: "unrelated command",
			cmd:  shell.CommandInfo{Name: "echo", Subcommand: "hello", Args: []string{"hello"}},
			want: false,
		},
		{
			// OS package manager installs are handled via InstallCommand,
			// not phpExtensionReferencesXdebug.
			name: "apt-get install php-xdebug ignored by ext matcher",
			cmd:  shell.CommandInfo{Name: "apt-get", Subcommand: "install", Args: []string{"install", "-y", "php-xdebug"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := phpExtensionReferencesXdebug(tt.cmd); got != tt.want {
				t.Errorf("phpExtensionReferencesXdebug(%v) = %v, want %v", tt.cmd.Name, got, tt.want)
			}
		})
	}
}

func TestArgsContainXdebug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "exact match", args: []string{"xdebug"}, want: true},
		{name: "versioned", args: []string{"xdebug-3.4.0"}, want: true},
		{name: "among others", args: []string{"gd", "xdebug", "intl"}, want: true},
		{name: "skip flags", args: []string{"-j$(nproc)", "xdebug"}, want: true},
		{name: "case insensitive", args: []string{"Xdebug"}, want: true},
		{name: "no match", args: []string{"gd", "intl"}, want: false},
		{name: "empty", args: nil, want: false},
		{name: "flags only", args: []string{"--enable-debug"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := argsContainXdebug(tt.args); got != tt.want {
				t.Errorf("argsContainXdebug(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestInstallCommandInstallsXdebug(t *testing.T) {
	t.Parallel()

	makeIC := func(manager string, names ...string) shell.InstallCommand {
		pkgs := make([]shell.PackageArg, 0, len(names))
		for _, n := range names {
			pkgs = append(pkgs, shell.PackageArg{Normalized: n})
		}
		return shell.InstallCommand{Manager: manager, Packages: pkgs}
	}

	tests := []struct {
		name string
		ic   shell.InstallCommand
		want bool
	}{
		{name: "apt php-xdebug", ic: makeIC("apt-get", "php-xdebug"), want: true},
		{name: "apt php8.3-xdebug", ic: makeIC("apt-get", "php8.3-xdebug"), want: true},
		{name: "apt php-xdebug with version spec", ic: makeIC("apt-get", "php-xdebug=3.1.6-1+deb12u1"), want: true},
		{name: "apt php-xdebug with arch", ic: makeIC("apt-get", "php-xdebug:amd64"), want: true},
		{name: "apk php83-pecl-xdebug", ic: makeIC("apk", "php83-pecl-xdebug"), want: true},
		{name: "dnf php-pecl-xdebug", ic: makeIC("dnf", "php-pecl-xdebug"), want: true},
		{name: "yum mixed packages", ic: makeIC("yum", "php-gd", "php-pecl-xdebug", "php-intl"), want: true},
		{name: "microdnf php-pecl-xdebug", ic: makeIC("microdnf", "php-pecl-xdebug"), want: true},
		{name: "zypper php-xdebug", ic: makeIC("zypper", "php-xdebug"), want: true},
		{name: "apt no xdebug", ic: makeIC("apt-get", "php-gd", "php-intl"), want: false},
		{name: "empty", ic: shell.InstallCommand{}, want: false},
		// Non-OS package managers must not count: an npm/pip/composer package
		// whose name happens to contain "xdebug" is not the PHP extension.
		{name: "npm fake xdebug package", ic: makeIC("npm", "xdebug"), want: false},
		{name: "pip fake xdebug package", ic: makeIC("pip", "php-xdebug"), want: false},
		{name: "composer fake xdebug package", ic: makeIC("composer", "php-xdebug"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := installCommandInstallsXdebug(tt.ic); got != tt.want {
				t.Errorf("installCommandInstallsXdebug(%v) = %v, want %v", tt.ic.Packages, got, tt.want)
			}
		})
	}
}

func TestPackagesOnlyXdebug(t *testing.T) {
	t.Parallel()

	makeIC := func(manager string, names ...string) shell.InstallCommand {
		pkgs := make([]shell.PackageArg, 0, len(names))
		for _, n := range names {
			pkgs = append(pkgs, shell.PackageArg{Normalized: n})
		}
		return shell.InstallCommand{Manager: manager, Packages: pkgs}
	}

	tests := []struct {
		name string
		ic   shell.InstallCommand
		want bool
	}{
		{name: "apt single xdebug", ic: makeIC("apt-get", "php-xdebug"), want: true},
		{name: "apt versioned xdebug", ic: makeIC("apt-get", "php-xdebug=3.1.6"), want: true},
		{name: "apt xdebug only (multi)", ic: makeIC("apt-get", "php-xdebug", "php8.3-xdebug"), want: true},
		{name: "apt mixed with non-xdebug", ic: makeIC("apt-get", "php-gd", "php-xdebug"), want: false},
		{name: "apt no xdebug", ic: makeIC("apt-get", "php-gd"), want: false},
		{name: "apt empty packages", ic: makeIC("apt-get"), want: false},
		{name: "npm not OS manager", ic: makeIC("npm", "xdebug"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := packagesOnlyXdebug(tt.ic); got != tt.want {
				t.Errorf("packagesOnlyXdebug(%v) = %v, want %v", tt.ic, got, tt.want)
			}
		})
	}
}

func TestComposerTruthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "zero", value: "0", want: false},
		{name: "false", value: "false", want: false},
		{name: "empty", value: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := composerTruthy(tt.value); got != tt.want {
				t.Errorf("composerTruthy(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
