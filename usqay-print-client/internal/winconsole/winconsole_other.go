//go:build !windows

package winconsole

func Hide()           {}
func Show()           {}
func IsVisible() bool { return false }
func Toggle()         {}
