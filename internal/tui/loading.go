package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// showLoadingScreen displays a loading screen with spinner and progress updates
// Returns a function to update the message and a channel to signal completion
func showLoadingScreen(app *tview.Application, title, initialMessage string, onCancel func()) (updateMsg func(string), done chan bool) {
	loadingText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	loadingText.SetBorder(true).SetTitle(title)

	spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerIdx := 0
	currentMessage := initialMessage

	updateLoadingText := func() {
		loadingText.SetText(fmt.Sprintf("\n\n[yellow]%s[-] %s\n\n[gray]Press Ctrl+C or Esc to cancel[-]",
			spinnerFrames[spinnerIdx], currentMessage))
		spinnerIdx = (spinnerIdx + 1) % len(spinnerFrames)
	}
	updateLoadingText()

	loadingFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(loadingText, 10, 0, true).
			AddItem(nil, 0, 1, false), 70, 0, true).
		AddItem(nil, 0, 1, false)

	// Set up cancel handler
	loadingText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
			if onCancel != nil {
				onCancel()
			}
			return nil
		}
		return event
	})

	app.SetRoot(loadingFlex, true).SetFocus(loadingText)

	// Force an immediate draw so the loading screen is visible right away
	// This is important when called from within a callback (like project selector)
	app.ForceDraw()

	// Start spinner animation
	spinnerDone := make(chan bool, 2)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				app.QueueUpdateDraw(updateLoadingText)
			case <-spinnerDone:
				return
			}
		}
	}()

	updateMessage := func(msg string) {
		currentMessage = msg
		app.QueueUpdateDraw(updateLoadingText)
	}

	return updateMessage, spinnerDone
}
