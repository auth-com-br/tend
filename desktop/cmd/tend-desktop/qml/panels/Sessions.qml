// The agents' sessions: the conversations the server's machine's agents
// keep, searched as the query is typed, as the TUI's sessions list. One is
// resumed in a new tab where it was held — or gone to, when it is open in
// a pane — handed on to another agent, or marked and deleted after asking.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: Object.assign({ total: 0, marked: 0 }, st.sessions || {}, { shown: (st.sessions && st.sessions.shown) || [], agents: (st.sessions && st.sessions.agents) || [] })
    // The conversation whose "continue in" is open.
    property string handing: ""
    title: "Agent sessions"
    subtitle: (s.shown.length === s.total ? s.total : s.shown.length + " of " + s.total) + " sessions"
              + (s.marked > 0 ? " · " + s.marked + " marked" : "")
    message: s.message || ""
    loading: !!s.loading
    confirm: s.confirm || ""
    boxWidth: 980
    onAnswered: (yes) => Bridge.call("sessions", { do: "answer", yes: yes })
    onClosed: Bridge.call("panel", { name: "" })

    Row {
        id: tools
        width: parent.width
        spacing: 6
        ToolField {
            id: query
            width: parent.width - buttons.width - 6
            hint: "search the conversations: a word, an agent, a folder"
            onEdited: Bridge.call("sessions", { do: "query", text: text })
            Component.onCompleted: focusIt()
        }
        Row {
            id: buttons
            spacing: 6
            anchors.verticalCenter: parent.verticalCenter
            ToolButton { label: "mark 30 days old"; onClicked: Bridge.call("sessions", { do: "mark-old" }) }
            ToolButton { label: "mark shown"; onClicked: Bridge.call("sessions", { do: "mark-all" }) }
            ToolButton { label: "delete marked"; danger: true; active: screen.s.marked > 0; onClicked: Bridge.call("sessions", { do: "delete" }) }
            ToolButton { label: "reload"; onClicked: Bridge.call("sessions", { do: "reload" }) }
        }
    }

    ListView {
        id: list
        y: tools.height + 10
        width: parent.width
        height: parent.height - y
        clip: true
        spacing: 2
        model: screen.s.shown
        boundsBehavior: Flickable.StopAtBounds
        delegate: Rectangle {
            id: row
            required property var modelData
            width: list.width
            height: 46
            radius: 6
            color: modelData.marked ? Qt.rgba(theme.red.r, theme.red.g, theme.red.b, 0.15)
                                    : (rowArea.containsMouse ? theme.surface0 : "transparent")
            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                onDoubleClicked: Bridge.call("sessions", { do: "resume", id: row.modelData.id })
            }
            // Marked for deleting; one open in a pane cannot be.
            Rectangle {
                x: 8
                anchors.verticalCenter: parent.verticalCenter
                width: 16
                height: 16
                radius: 3
                visible: row.modelData.pane === undefined || row.modelData.pane === 0
                color: row.modelData.marked ? theme.red : "transparent"
                border.color: theme.overlay0
                MouseArea { anchors.fill: parent; anchors.margins: -4; onClicked: Bridge.call("sessions", { do: "mark", id: row.modelData.id }) }
            }
            Column {
                x: 34
                anchors.verticalCenter: parent.verticalCenter
                width: row.width - 34 - rowButtons.width - 16
                Text {
                    width: parent.width
                    text: (row.modelData.title || "(untitled)")
                    color: theme.text
                    font.pixelSize: 13
                    elide: Text.ElideRight
                }
                Text {
                    width: parent.width
                    text: row.modelData.agent + " · " + row.modelData.age + " · " + row.modelData.prompts + " prompts · "
                          + row.modelData.size + " · " + row.modelData.dir
                          + (row.modelData.pane ? " · open in pane " + row.modelData.pane : "")
                          + (row.modelData.unreachable ? " · " + row.modelData.unreachable : "")
                    color: row.modelData.unreachable ? theme.yellow : theme.overlay0
                    font.pixelSize: 11
                    elide: Text.ElideMiddle
                }
            }
            Row {
                id: rowButtons
                anchors.right: parent.right
                anchors.rightMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                spacing: 4
                ToolButton {
                    label: row.modelData.pane ? "go to" : "resume"
                    primary: true
                    onClicked: Bridge.call("sessions", { do: "resume", id: row.modelData.id })
                }
                ToolButton {
                    label: "continue in…"
                    visible: row.modelData.agent === "claude"
                    onClicked: screen.handing = row.modelData.id
                }
                ToolButton {
                    label: "delete"
                    danger: true
                    active: !row.modelData.pane
                    onClicked: Bridge.call("sessions", { do: "delete", id: row.modelData.id })
                }
            }
        }
        Text {
            visible: !screen.s.loading && screen.s.shown.length === 0
            y: 10
            x: 8
            text: screen.s.total === 0 ? "No conversation kept on this machine yet." : "Nothing matches the search."
            color: theme.overlay0
            font.pixelSize: 13
        }
    }

    // The agents a conversation can be handed to.
    Rectangle {
        visible: screen.handing !== ""
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.35)
        MouseArea { anchors.fill: parent; onClicked: screen.handing = "" }
        Rectangle {
            anchors.centerIn: parent
            width: 320
            height: 50 + screen.s.agents.length * 32
            radius: 8
            color: theme.mantle
            border.color: theme.surface1
            MouseArea { anchors.fill: parent }
            Text { x: 14; y: 12; text: "continue in"; color: theme.text; font.bold: true; font.pixelSize: 14 }
            Column {
                x: 8
                y: 40
                width: parent.width - 16
                Repeater {
                    model: screen.s.agents
                    delegate: Rectangle {
                        required property string modelData
                        width: parent.width
                        height: 30
                        radius: 5
                        color: agentArea.containsMouse ? theme.surface0 : "transparent"
                        Text { x: 8; anchors.verticalCenter: parent.verticalCenter; text: modelData; color: theme.text; font.pixelSize: 13 }
                        MouseArea {
                            id: agentArea
                            anchors.fill: parent
                            hoverEnabled: true
                            onClicked: {
                                Bridge.call("sessions", { do: "continue", id: screen.handing, agent: modelData })
                                screen.handing = ""
                            }
                        }
                    }
                }
            }
        }
    }
}
