package styles

import "github.com/charmbracelet/lipgloss"

type ColorPalette struct {
	Primary     string
	Accent      string
	Muted       string
	Fg          string
	DimFg       string
	Error       string
	Warning     string
	Success     string
	SelectionBg string
	SelectionFg string
	UnreadFg    string
}

type Theme struct {
	Header        lipgloss.Style
	StatusBar     lipgloss.Style
	ListTitle     lipgloss.Style
	Unread        lipgloss.Style
	Muted         lipgloss.Style
	Label         lipgloss.Style
	Separator     lipgloss.Style
	Pane          lipgloss.Style
	FocusedPane   lipgloss.Style
	SelectedTitle lipgloss.Style
	SelectedDesc  lipgloss.Style
	Error         lipgloss.Style
	Warning       lipgloss.Style
	Success       lipgloss.Style
}

func Build(p ColorPalette) Theme {
	primary := lipgloss.Color(p.Primary)
	muted := lipgloss.Color(p.Muted)
	fg := lipgloss.Color(p.Fg)
	dimFg := lipgloss.Color(p.DimFg)
	selBg := lipgloss.Color(p.SelectionBg)
	selFg := lipgloss.Color(p.SelectionFg)
	unreadFg := lipgloss.Color(p.UnreadFg)
	errColor := lipgloss.Color(p.Error)
	warnColor := lipgloss.Color(p.Warning)
	succColor := lipgloss.Color(p.Success)

	return Theme{
		Header: lipgloss.NewStyle().
			Background(primary).
			Foreground(fg).
			Bold(true).
			Padding(0, 1),
		StatusBar: lipgloss.NewStyle().
			Background(muted).
			Foreground(dimFg).
			Padding(0, 1),
		ListTitle: lipgloss.NewStyle().
			Foreground(primary).
			Bold(true).
			Padding(0, 1),
		Unread: lipgloss.NewStyle().
			Foreground(unreadFg).
			Bold(true),
		Muted: lipgloss.NewStyle().
			Foreground(dimFg),
		Label: lipgloss.NewStyle().
			Foreground(dimFg),
		Separator: lipgloss.NewStyle().
			Foreground(dimFg),
		Pane: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(muted),
		FocusedPane: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(primary),
		SelectedTitle: lipgloss.NewStyle().
			Foreground(selFg).
			Background(selBg).
			Bold(true),
		SelectedDesc: lipgloss.NewStyle().
			Foreground(selFg).
			Background(selBg),
		Error: lipgloss.NewStyle().
			Foreground(errColor),
		Warning: lipgloss.NewStyle().
			Foreground(warnColor),
		Success: lipgloss.NewStyle().
			Foreground(succColor),
	}
}
