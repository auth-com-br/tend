package main

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"

	"github.com/auth-com-br/tend/internal/launcher"

	"github.com/auth-com-br/tend/desktop/internal/core"
)

// tray is the window's icon in the desktop's system tray (#71): how many
// agents wait for you, in its tooltip and its menu, with the window behind
// others or minimised — and a click on "go to" takes you to the one that
// has waited longest. Closing the window still quits; the tray is there
// while the window is, not instead of it.
type tray struct {
	icon    *qt.QSystemTrayIcon
	waiting *qt.QAction
	goTo    *qt.QAction
	oldest  uint64
}

// newTray puts the icon up, when the desktop has a tray. Main thread.
func newTray(sess *core.Session, window *qt.QWindow, session string) *tray {
	if !qt.QSystemTrayIcon_IsSystemTrayAvailable() {
		logf("tray: the desktop has none")
		return nil
	}
	pixmap := qt.NewQPixmap()
	pixmap.LoadFromData4(launcher.Icon(), "SVG")
	t := &tray{icon: qt.NewQSystemTrayIcon2(qt.NewQIcon2(pixmap))}
	menu := qt.NewQMenu2()
	show := menu.AddActionWithText("Show tend — " + session)
	show.OnTriggered(func() { raiseWindow(window) })
	t.waiting = menu.AddActionWithText("no agent waiting")
	t.waiting.SetEnabled(false)
	t.goTo = menu.AddActionWithText("Go to the agent waiting longest")
	t.goTo.SetEnabled(false)
	t.goTo.OnTriggered(func() {
		if t.oldest == 0 {
			return
		}
		raiseWindow(window)
		pane := t.oldest
		background(func() error { return sess.JumpToPane(pane) })
	})
	menu.AddSeparator()
	quit := menu.AddActionWithText("Quit the window (the session keeps running)")
	quit.OnTriggered(func() { qt.QCoreApplication_Quit() })
	t.icon.SetContextMenu(menu)
	t.icon.OnActivated(func(reason qt.QSystemTrayIcon__ActivationReason) {
		if reason == qt.QSystemTrayIcon__Trigger {
			raiseWindow(window)
		}
	})
	t.icon.SetToolTip("tend — " + session)
	t.icon.Show()
	logf("tray: up")
	return t
}

// update says how many agents wait. Main thread.
func (t *tray) update(session string, count int, oldest uint64) {
	if t == nil {
		return
	}
	t.oldest = oldest
	tip := "tend — " + session
	switch count {
	case 0:
		t.waiting.SetText("no agent waiting")
	case 1:
		t.waiting.SetText("1 agent waiting for you")
		tip += " · 1 waiting"
	default:
		t.waiting.SetText(fmt.Sprintf("%d agents waiting for you", count))
		tip += fmt.Sprintf(" · %d waiting", count)
	}
	t.goTo.SetEnabled(count > 0)
	t.icon.SetToolTip(tip)
}

// raiseWindow brings the window up and to the front. Main thread.
func raiseWindow(w *qt.QWindow) {
	w.Show()
	w.Raise()
	w.RequestActivate()
}
