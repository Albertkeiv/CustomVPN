package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// windows11Theme задает мягкую палитру и скругления в духе Windows 11.
type windows11Theme struct {
	base fyne.Theme
}

func newWindows11Theme() fyne.Theme {
	return &windows11Theme{base: theme.LightTheme()}
}

func (t *windows11Theme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 244, G: 244, B: 249, A: 255}
	case theme.ColorNameButton, theme.ColorNamePrimary:
		return color.NRGBA{R: 37, G: 99, B: 235, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 22, G: 24, B: 35, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 180, G: 184, B: 193, A: 255}
	default:
		return t.base.Color(name, variant)
	}
}

func (t *windows11Theme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *windows11Theme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *windows11Theme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}
