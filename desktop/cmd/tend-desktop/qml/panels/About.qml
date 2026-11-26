// About tend: which build the window is and which the server is — the
// server outlives its clients, and one left behind by an update is the
// first thing to know when something is missing — where tend lives, and
// its licence, as the TUI's about panel says it.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: st.about || ({})
    title: "About tend"
    boxWidth: 560
    boxHeight: 380
    onClosed: Bridge.call("panel", { name: "" })

    Column {
        width: parent.width
        spacing: 8
        Text {
            text: "tend-desktop " + (screen.s.version || "")
            color: theme.text
            font.pixelSize: 15
            font.bold: true
        }
        Text {
            text: "server " + (screen.s.server || "unknown") + (screen.s.stale ? "  ●  another build than this window" : "")
            color: screen.s.stale ? theme.yellow : theme.subtext
            font.pixelSize: 13
        }
        Text {
            visible: !!screen.s.stale
            text: (screen.s.hint || "") + " to bring the server to this build, keeping the programs in the panes"
            color: theme.subtext
            font.pixelSize: 12
            width: parent.width
            wrapMode: Text.Wrap
        }
        Item { width: 1; height: 6 }
        Text { text: "A terminal runtime for AI coding agents."; color: theme.text; font.pixelSize: 13 }
        Link { label: "site"; url: screen.s.site || "" }
        Link { label: "source"; url: screen.s.source || "" }
        Link { label: "sponsor"; url: screen.s.sponsor || "" }
        Item { width: 1; height: 6 }
        Text {
            text: "Apache License 2.0. A port of herdr; see NOTICE."
            color: theme.overlay0
            font.pixelSize: 12
        }
        Row {
            spacing: 6
            ToolButton {
                visible: !!screen.s.notes
                label: "what's new"
                onClicked: Bridge.call("panel", { name: "notes" })
            }
            ToolButton { label: "close"; onClicked: Bridge.call("panel", { name: "" }) }
        }
    }

    component Link: Row {
        id: link
        property string label: ""
        property string url: ""
        spacing: 8
        Text { width: 60; text: link.label; color: theme.overlay0; font.pixelSize: 12 }
        Text {
            text: link.url
            color: linkArea.containsMouse ? theme.accent : theme.text
            font.pixelSize: 12
            font.underline: linkArea.containsMouse
            MouseArea {
                id: linkArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: Qt.openUrlExternally(link.url)
            }
        }
    }
}
