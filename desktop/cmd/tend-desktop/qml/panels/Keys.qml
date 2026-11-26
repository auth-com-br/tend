// The window's keyboard shortcuts: the TUI's "keybinds", for the window.
// They are not the TUI's prefix keys — in the window every key but these
// goes to the pane in focus, as in a desktop terminal — so the screen says
// which keys are the window's, and what the mouse does.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    title: "Keyboard shortcuts"
    boxWidth: 560
    boxHeight: 640
    onClosed: Bridge.call("panel", { name: "" })

    readonly property var keys: [
        { k: "Ctrl+Shift+P", d: "Command palette: every action, by name" },
        { k: "Ctrl+Shift+T", d: "New tab" },
        { k: "Ctrl+Shift+W", d: "Close the pane in focus" },
        { k: "Ctrl+PgDown / Ctrl+PgUp", d: "Next tab / previous tab" },
        { k: "Ctrl+Shift+E", d: "Split to the right" },
        { k: "Ctrl+Shift+O", d: "Split down" },
        { k: "Ctrl+Shift+B", d: "Files tab" },
        { k: "Ctrl+Shift+V", d: "Paste into the pane" },
        { k: "Shift+Enter", d: "A new line in an agent's prompt" }
    ]
    readonly property var mouse: [
        { k: "Right click", d: "The menu of a pane, a tab, a space, an agent or a file" },
        { k: "Select with the mouse", d: "Copies what is selected" },
        { k: "Drag a tab out", d: "The tab in a window of its own" },
        { k: "Drop files on an agent", d: "Gives it their paths" },
        { k: "Drag a divider", d: "Resizes the panes, the sidebar or the dock" }
    ]

    Column {
        width: parent.width
        spacing: 4
        Text {
            width: parent.width
            text: "Every other key goes to the pane in focus."
            color: theme.subtext
            font.pixelSize: 12
            wrapMode: Text.Wrap
            bottomPadding: 6
        }
        Repeater { model: screen.keys; delegate: KeyRow {} }
        Item { width: 1; height: 10 }
        Text { text: "Mouse"; color: theme.subtext; font.pixelSize: 12; font.weight: Font.DemiBold; bottomPadding: 4 }
        Repeater { model: screen.mouse; delegate: KeyRow {} }
    }

    component KeyRow: Item {
        required property var modelData
        width: parent ? parent.width : 0
        height: 28
        Rectangle {
            id: cap
            anchors.verticalCenter: parent.verticalCenter
            width: Math.max(180, capText.implicitWidth + 16)
            height: 22
            radius: 5
            color: theme.surface0
            border.width: 1
            border.color: theme.surface1
            Text {
                id: capText
                anchors.centerIn: parent
                text: modelData.k
                color: theme.text
                font.pixelSize: 11
                font.weight: Font.DemiBold
            }
        }
        Text {
            anchors.left: cap.right
            anchors.leftMargin: 12
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            text: modelData.d
            color: theme.text
            font.pixelSize: 12
            elide: Text.ElideRight
        }
    }
}
