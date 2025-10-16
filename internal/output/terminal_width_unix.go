//go:build !windows

package output

import (
	"os"

	"golang.org/x/sys/unix"
)

func systemTerminalWidth() (int, bool) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil || ws.Col == 0 {
		return 0, false
	}

	return int(ws.Col), true
}
