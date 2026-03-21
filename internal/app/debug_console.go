package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const maxDebugConsoleLines = 1200

type debugConsoleState struct {
	mutex            sync.RWMutex
	lines            []string
	logFilePath      string
	subscribers      map[int]func([]string)
	nextSubscriberID int
}

var debugConsole = &debugConsoleState{
	lines:       []string{},
	subscribers: map[int]func([]string){},
}

func logDebugf(format string, args ...any) {
	messageText := fmt.Sprintf(format, args...)
	debugConsole.appendLine(messageText)
}

func (state *debugConsoleState) appendLine(messageText string) {
	state.mutex.Lock()
	timestampedLine := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), strings.TrimSpace(messageText))
	state.lines = append(state.lines, timestampedLine)
	if len(state.lines) > maxDebugConsoleLines {
		state.lines = state.lines[len(state.lines)-maxDebugConsoleLines:]
	}
	linesSnapshot := append([]string(nil), state.lines...)
	subscriberList := make([]func([]string), 0, len(state.subscribers))
	for _, subscriber := range state.subscribers {
		subscriberList = append(subscriberList, subscriber)
	}
	state.mutex.Unlock()

	for _, subscriber := range subscriberList {
		subscriber(linesSnapshot)
	}

	state.mutex.RLock()
	logFilePath := state.logFilePath
	state.mutex.RUnlock()
	if strings.TrimSpace(logFilePath) != "" {
		appendLineToLogFile(logFilePath, timestampedLine)
	}
}

func (state *debugConsoleState) clear() {
	state.mutex.Lock()
	state.lines = []string{}
	subscriberList := make([]func([]string), 0, len(state.subscribers))
	for _, subscriber := range state.subscribers {
		subscriberList = append(subscriberList, subscriber)
	}
	state.mutex.Unlock()

	for _, subscriber := range subscriberList {
		subscriber([]string{})
	}
}

func (state *debugConsoleState) subscribe(onUpdate func([]string)) func() {
	state.mutex.Lock()
	subscriberID := state.nextSubscriberID
	state.nextSubscriberID++
	state.subscribers[subscriberID] = onUpdate
	linesSnapshot := append([]string(nil), state.lines...)
	state.mutex.Unlock()

	onUpdate(linesSnapshot)
	return func() {
		state.mutex.Lock()
		delete(state.subscribers, subscriberID)
		state.mutex.Unlock()
	}
}

func initializeDebugLogFile() {
	repositoryRootPath, rootErr := getRepositoryRootPath()
	if rootErr != nil {
		return
	}
	logFilePath := filepath.Join(repositoryRootPath, "latest.log")
	_ = os.WriteFile(logFilePath, []byte(""), 0644)
	debugConsole.mutex.Lock()
	debugConsole.logFilePath = logFilePath
	debugConsole.mutex.Unlock()
}

func appendLineToLogFile(logFilePath string, line string) {
	logFileHandle, openErr := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if openErr != nil {
		return
	}
	defer logFileHandle.Close()
	_, _ = logFileHandle.WriteString(line + "\n")
}

func newDebugConsolePanel(onVisibilityChanged func(bool)) fyne.CanvasObject {
	consoleGrid := widget.NewTextGrid()
	consoleGrid.SetText("Debug logs will appear here...")

	isVisible := false
	toggleButton := widget.NewButton("Show Debug Console", nil)
	clearButton := widget.NewButton("Clear", func() {
		debugConsole.clear()
	})
	clearButton.Disable()

	consoleScroll := container.NewVScroll(consoleGrid)
	consoleScroll.Hide()

	renderLines := func(lines []string) {
		fyne.Do(func() {
			if len(lines) == 0 {
				consoleGrid.SetText("Debug logs will appear here...")
				return
			}
			consoleGrid.SetText(strings.Join(lines, "\n"))
		})
	}
	unsubscribe := debugConsole.subscribe(renderLines)

	toggleButton.OnTapped = func() {
		isVisible = !isVisible
		if isVisible {
			consoleScroll.Show()
			clearButton.Enable()
			toggleButton.SetText("Hide Debug Console")
			if onVisibilityChanged != nil {
				onVisibilityChanged(true)
			}
			logDebugf("Debug console shown")
			return
		}
		consoleScroll.Hide()
		clearButton.Disable()
		toggleButton.SetText("Show Debug Console")
		if onVisibilityChanged != nil {
			onVisibilityChanged(false)
		}
		logDebugf("Debug console hidden")
	}

	header := container.NewVBox(
		widget.NewSeparator(),
		container.NewHBox(toggleButton, clearButton),
	)
	panel := container.NewBorder(header, nil, nil, nil, consoleScroll)
	_ = unsubscribe
	return panel
}
