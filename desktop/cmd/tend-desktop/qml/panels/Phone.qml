// The phone view: the TUI's menu item (cmd/tend/tui_phone.go), as a
// screen. The session's server runs `tend web`; this says whether it is
// on, where to open it from the phone, and turns it on or off. It is the
// server's, so it is the same view the TUI turns on.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var p: st.phone || ({})
    title: "Phone view"
    boxWidth: 560
    boxHeight: 330
    onClosed: Bridge.call("panel", { name: "" })

    Column {
        width: parent.width
        spacing: 10
        Text {
            width: parent.width
            text: "The session in a browser on your phone: the agents, what they are doing, and an answer when one waits for you."
            color: theme.subtext
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
        Row {
            spacing: 8
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                width: 10
                height: 10
                radius: 5
                color: screen.p.running ? theme.green : screen.p.on ? theme.yellow : theme.overlay0
            }
            Text {
                text: !screen.p.loaded ? "asking the server…"
                    : screen.p.busy ? screen.p.busy
                    : screen.p.running ? "on" : screen.p.on ? "on, not running" : "off"
                color: theme.text
                font.pixelSize: 14
                font.weight: Font.DemiBold
            }
        }
        Text {
            visible: !!screen.p.error
            width: parent.width
            text: screen.p.error || ""
            color: theme.red
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
        Text {
            visible: (screen.p.urls || []).length > 0
            text: "Open on the phone (a click copies):"
            color: theme.subtext
            font.pixelSize: 12
        }
        Repeater {
            model: screen.p.urls || []
            delegate: Rectangle {
                required property var modelData
                required property int index
                width: urlText.implicitWidth + 20
                height: 28
                radius: 6
                color: urlArea.containsMouse ? theme.surface1 : theme.surface0
                Text {
                    id: urlText
                    anchors.centerIn: parent
                    text: modelData
                    color: theme.accent
                    font.pixelSize: 13
                }
                MouseArea {
                    id: urlArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: Bridge.call("phone", { do: "copy", index: index })
                }
            }
        }
        // What the TUI's panel says (internal/ui/phoneview.go), in its words.
        Text {
            visible: !!screen.p.loaded && !screen.p.running && !screen.p.error
            width: parent.width
            text: screen.p.on ? "" : "Turned on, it serves a page to your network or VPN ([web] listen, port 7777 unless set), and comes back when tend's server restarts."
            color: theme.overlay0
            font.pixelSize: 11
            wrapMode: Text.Wrap
        }
        Text {
            visible: !!screen.p.running && (screen.p.urls || []).length === 0
            text: "Running, but this machine has no network a phone can reach."
            color: theme.overlay0
            font.pixelSize: 11
        }
        Text {
            visible: !!screen.p.loaded
            width: parent.width
            text: screen.p.user ? "Or open it without the address's token and sign in as " + screen.p.user + "."
                                : "No login set: `tend web -set-password` sets one, to sign in instead."
            color: theme.overlay0
            font.pixelSize: 11
            wrapMode: Text.Wrap
        }
        Row {
            spacing: 6
            ToolButton {
                primary: !screen.p.on
                danger: !!screen.p.on
                active: !!screen.p.loaded && !screen.p.busy
                label: screen.p.on ? "turn off" : "turn on"
                onClicked: Bridge.call("phone", { do: "toggle" })
            }
            ToolButton { label: "close"; onClicked: Bridge.call("panel", { name: "" }) }
        }
    }
}
