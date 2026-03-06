package theme

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wharflab/tally/internal/highlight/core"
)

type Mode string

const (
	ModeAuto  Mode = "auto"
	ModeDark  Mode = "dark"
	ModeLight Mode = "light"
)

type Palette struct {
	Mode    Mode
	ByToken map[core.TokenType]lipgloss.Style
	Base    lipgloss.Style
	Overlay lipgloss.Style
}

func Resolve(enabled bool, mode string) Palette {
	if !enabled {
		return Palette{}
	}

	selected := parseMode(mode)
	if selected == ModeAuto {
		switch strings.ToLower(os.Getenv("TALLY_THEME")) {
		case string(ModeDark):
			selected = ModeDark
		case string(ModeLight):
			selected = ModeLight
		default:
			if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
				selected = ModeDark
			} else {
				selected = ModeLight
			}
		}
	}
	if selected == "" {
		selected = ModeDark
	}

	if selected == ModeLight {
		return lightPalette()
	}
	return darkPalette()
}

func parseMode(mode string) Mode {
	switch Mode(strings.ToLower(mode)) {
	case "", ModeAuto:
		return ModeAuto
	case ModeDark:
		return ModeDark
	case ModeLight:
		return ModeLight
	default:
		return ModeAuto
	}
}

func darkPalette() Palette {
	return newPalette(ModeDark, tokenStyles(tokenStyleConfig{
		keyword:   "81",
		comment:   "244",
		str:       "114",
		number:    "179",
		operator:  "215",
		variable:  "221",
		parameter: "177",
		property:  "75",
		function:  "218",
	}))
}

func lightPalette() Palette {
	return newPalette(ModeLight, tokenStyles(tokenStyleConfig{
		keyword:   "25",
		comment:   "245",
		str:       "28",
		number:    "94",
		operator:  "166",
		variable:  "130",
		parameter: "97",
		property:  "24",
		function:  "161",
	}))
}

func newPalette(mode Mode, tokenStyles map[core.TokenType]lipgloss.Style) Palette {
	return Palette{
		Mode: mode,
		Base: lipgloss.NewStyle(),
		Overlay: lipgloss.NewStyle().
			Bold(true).
			Underline(true),
		ByToken: tokenStyles,
	}
}

type tokenStyleConfig struct {
	keyword   string
	comment   string
	str       string
	number    string
	operator  string
	variable  string
	parameter string
	property  string
	function  string
}

func tokenStyles(cfg tokenStyleConfig) map[core.TokenType]lipgloss.Style {
	return map[core.TokenType]lipgloss.Style{
		core.TokenKeyword:   lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.keyword)).Bold(true),
		core.TokenComment:   lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.comment)).Italic(true),
		core.TokenString:    lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.str)),
		core.TokenNumber:    lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.number)),
		core.TokenOperator:  lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.operator)),
		core.TokenVariable:  lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.variable)),
		core.TokenParameter: lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.parameter)),
		core.TokenProperty:  lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.property)),
		core.TokenFunction:  lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.function)),
	}
}
