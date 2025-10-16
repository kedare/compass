//go:build windows

package output

func systemTerminalWidth() (int, bool) {
	return 0, false
}
