//go:build windows

package ui

import "golang.org/x/sys/windows"

// enableVT turns on ANSI escape processing for the Windows console so
// color output renders instead of leaking raw escape codes.
func enableVT() {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return
	}
	windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
