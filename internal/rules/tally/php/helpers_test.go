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

func TestCommandReferencesXdebug(t *testing.T) {
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
			name: "apt-get install php-xdebug",
			cmd:  shell.CommandInfo{Name: "apt-get", Subcommand: "install", Args: []string{"install", "-y", "php-xdebug"}},
			want: true,
		},
		{
			name: "apt-get install versioned php8.3-xdebug",
			cmd:  shell.CommandInfo{Name: "apt-get", Subcommand: "install", Args: []string{"install", "php8.3-xdebug"}},
			want: true,
		},
		{
			name: "apk add php-pecl-xdebug",
			cmd:  shell.CommandInfo{Name: "apk", Subcommand: "add", Args: []string{"add", "--no-cache", "php83-pecl-xdebug"}},
			want: true,
		},
		{
			name: "dnf install php-pecl-xdebug",
			cmd:  shell.CommandInfo{Name: "dnf", Subcommand: "install", Args: []string{"install", "php-pecl-xdebug"}},
			want: true,
		},
		{
			name: "yum install php-pecl-xdebug",
			cmd:  shell.CommandInfo{Name: "yum", Subcommand: "install", Args: []string{"install", "php-pecl-xdebug"}},
			want: true,
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
			name: "apt-get update",
			cmd:  shell.CommandInfo{Name: "apt-get", Subcommand: "update", Args: []string{"update"}},
			want: false,
		},
		{
			name: "unrelated command",
			cmd:  shell.CommandInfo{Name: "echo", Subcommand: "hello", Args: []string{"hello"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := commandReferencesXdebug(tt.cmd); got != tt.want {
				t.Errorf("commandReferencesXdebug(%v) = %v, want %v", tt.cmd.Name, got, tt.want)
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

func TestArgsContainXdebugSubstring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "php-xdebug", args: []string{"php-xdebug"}, want: true},
		{name: "php8.3-xdebug", args: []string{"php8.3-xdebug"}, want: true},
		{name: "php-pecl-xdebug", args: []string{"php-pecl-xdebug"}, want: true},
		{name: "php83-pecl-xdebug", args: []string{"php83-pecl-xdebug"}, want: true},
		{name: "skip flags", args: []string{"-y", "php-xdebug"}, want: true},
		{name: "no xdebug", args: []string{"php-gd", "php-intl"}, want: false},
		{name: "empty", args: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := argsContainXdebugSubstring(tt.args); got != tt.want {
				t.Errorf("argsContainXdebugSubstring(%v) = %v, want %v", tt.args, got, tt.want)
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
