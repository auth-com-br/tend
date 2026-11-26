// The first-run welcome, herdr's onboarding as the TUI has it, in the
// window's words: what the mouse does here instead of the prefix keys.
// Going on writes onboarding = false, for the TUI too, and opens the
// settings at the agents' integrations.
import QtQuick
import Tend 1.0

Rectangle {
    color: Qt.rgba(0, 0, 0, 0.55)
    MouseArea { anchors.fill: parent }
    Keys.onReturnPressed: Bridge.call("welcome.done", {})
    Component.onCompleted: forceActiveFocus()

    Rectangle {
        anchors.centerIn: parent
        width: 520
        height: body.height + 56
        radius: 12
        color: theme.mantle
        border.color: theme.surface1
        Column {
            id: body
            x: 32
            y: 28
            width: parent.width - 64
            spacing: 10
            Text { text: "tend"; color: theme.accent; font.pixelSize: 28; font.bold: true }
            Text { text: "a workspace for coding agents"; color: theme.subtext; font.pixelSize: 14 }
            Item { width: 1; height: 6 }
            Repeater {
                model: [
                    "Click the sidebar to switch spaces; drag the borders between panes to resize them.",
                    "Right-click a space, an agent, a tab or a pane for what it can do.",
                    "+ opens a terminal, an agent this machine has, or a web page.",
                    "An agent that needs you is said in a notification, even with the window behind others.",
                    "The terminal tend is the same session: open either, or both."
                ]
                delegate: Row {
                    required property string modelData
                    width: body.width
                    spacing: 10
                    Text { text: "•"; color: theme.accent; font.pixelSize: 14 }
                    Text {
                        width: parent.width - 20
                        text: modelData
                        color: theme.text
                        font.pixelSize: 14
                        wrapMode: Text.Wrap
                    }
                }
            }
            Item { width: 1; height: 6 }
            Text {
                width: body.width
                text: "Next: install the agents' integrations, for a more reliable state."
                color: theme.subtext
                font.pixelSize: 13
                wrapMode: Text.Wrap
            }
            Rectangle {
                width: 140
                height: 36
                radius: 8
                color: goArea.containsMouse ? Qt.lighter(theme.accent, 1.1) : theme.accent
                Text { anchors.centerIn: parent; text: "Continue"; color: theme.base; font.pixelSize: 14; font.bold: true }
                MouseArea {
                    id: goArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: Bridge.call("welcome.done", {})
                }
            }
        }
    }
}
