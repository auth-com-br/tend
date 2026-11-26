// The context buffer: what tools captured — the files panel, the browser,
// `tend context add`, a handoff's note — newest first, as the TUI's
// context panel lists it. An item is copied, sent to the agent being worked
// with (typed into its pane, not submitted, to be read over), or dropped.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: Object.assign({ items: [] }, st.context || {}, { items: (st.context && st.context.items) || [] })
    property int chosen: 0
    readonly property var item: s.items.length > 0 ? s.items[Math.min(chosen, s.items.length - 1)] : null
    title: "Context"
    subtitle: s.target ? "send goes to pane " + s.target : "no pane to send to in this space"
    message: s.message || ""
    loading: !!s.loading
    boxWidth: 980
    onClosed: Bridge.call("panel", { name: "" })

    function act(what) { Bridge.call("context", { do: what, id: screen.item ? screen.item.id : 0 }) }

    Row {
        id: tools
        width: parent.width
        spacing: 6
        ToolButton { label: "send"; primary: true; active: !!screen.item; onClicked: screen.act("send") }
        ToolButton { label: "send all"; active: screen.s.items.length > 0; onClicked: screen.act("send-all") }
        ToolButton { label: "copy"; active: !!screen.item; onClicked: screen.act("copy") }
        ToolButton { label: "remove"; active: !!screen.item; onClicked: screen.act("remove") }
        ToolButton {
            label: screen.s.clearArmed ? "clear: sure?" : "clear"
            danger: true
            active: screen.s.items.length > 0
            onClicked: screen.act("clear")
        }
        ToolButton { label: "reload"; onClicked: screen.act("reload") }
    }

    ListView {
        id: list
        y: tools.height + 10
        width: 340
        height: parent.height - y
        clip: true
        spacing: 2
        model: screen.s.items
        boundsBehavior: Flickable.StopAtBounds
        delegate: Rectangle {
            required property var modelData
            required property int index
            readonly property bool on: index === Math.min(screen.chosen, screen.s.items.length - 1)
            width: list.width
            height: 40
            radius: 6
            color: on ? theme.surface1 : (itemArea.containsMouse ? theme.surface0 : "transparent")
            Text {
                x: 10
                y: 5
                text: modelData.kind
                color: theme.accent
                font.pixelSize: 10
                font.bold: true
            }
            Text {
                x: 10
                y: 19
                width: parent.width - 20
                text: modelData.summary
                color: theme.text
                font.pixelSize: 12
                elide: Text.ElideRight
            }
            MouseArea { id: itemArea; anchors.fill: parent; hoverEnabled: true; onClicked: screen.chosen = index }
        }
        Text {
            visible: !screen.s.loading && screen.s.items.length === 0
            y: 10
            x: 8
            width: list.width - 16
            text: "Nothing captured. The files panel, the browser extension and `tend context add` put things here."
            color: theme.overlay0
            font.pixelSize: 13
            wrapMode: Text.Wrap
        }
    }

    // The chosen item's parts under their headings.
    Rectangle {
        x: list.width + 10
        y: list.y
        width: parent.width - x
        height: list.height
        radius: 6
        color: theme.base
        Flickable {
            anchors.fill: parent
            anchors.margins: 12
            contentHeight: parts.height
            clip: true
            Column {
                id: parts
                width: parent.width
                spacing: 10
                Repeater {
                    model: screen.item ? (screen.item.parts || []) : []
                    delegate: Column {
                        required property var modelData
                        width: parts.width
                        spacing: 2
                        Text { text: modelData.Heading; color: theme.overlay0; font.pixelSize: 10; font.bold: true }
                        Text {
                            width: parent.width
                            text: modelData.Value
                            color: theme.text
                            font.family: modelData.Heading === "TEXT" ? "monospace" : ""
                            font.pixelSize: 12
                            wrapMode: Text.Wrap
                            textFormat: Text.PlainText
                        }
                    }
                }
            }
        }
    }
}
