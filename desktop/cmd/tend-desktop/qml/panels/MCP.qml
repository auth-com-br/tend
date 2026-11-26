// The MCP servers: a row a server, a column an agent, as the TUI's MCP
// manager lays them out (internal/tools). A click on a cell turns the
// server off or on in that agent, or offers to copy it there from one that
// has it; a row's buttons test, show and remove it. Changes are written by
// the session's server and used from each agent's next session.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: Object.assign({}, st.mcp || {}, { agents: (st.mcp && st.mcp.agents) || [], rows: (st.mcp && st.mcp.rows) || [] })
    readonly property int nameW: 200
    readonly property int colW: 74
    // The agent a new server goes to: -1 is every agent.
    property int addTo: -1
    title: "MCP servers"
    subtitle: s.legend
    message: s.message || ""
    alert: !!s.alert
    loading: !!s.loading
    confirm: s.confirm || ""
    detail: s.detail || []
    onAnswered: (yes) => Bridge.call("mcp", { do: "answer", yes: yes })
    onClosed: Bridge.call("panel", { name: "" })
    onDetailClosed: Bridge.call("mcp", { do: "reload" })

    function act(what, row, agent, text) {
        Bridge.call("mcp", { do: what, row: row, agent: agent, text: text || "" })
    }

    Row {
        id: tools
        width: parent.width
        spacing: 6
        ToolButton { label: "test all"; onClicked: screen.act("test-all", -1, -1) }
        ToolButton { label: "reload"; onClicked: screen.act("reload", -1, -1) }
        ToolButton { label: "what is this?"; onClicked: screen.act("help", -1, -1) }
    }

    // The columns' names.
    Row {
        id: head
        y: tools.height + 12
        x: 8
        Text { width: screen.nameW; text: "server"; color: theme.overlay0; font.pixelSize: 12 }
        Repeater {
            model: screen.s.agents
            delegate: Text {
                required property string modelData
                width: screen.colW
                text: modelData
                color: theme.overlay0
                font.pixelSize: 12
                elide: Text.ElideRight
                horizontalAlignment: Text.AlignHCenter
            }
        }
        Text { width: 70; text: "  via"; color: theme.overlay0; font.pixelSize: 12 }
        Text { text: "test"; color: theme.overlay0; font.pixelSize: 12 }
    }

    ListView {
        id: list
        y: head.y + head.height + 6
        width: parent.width
        height: parent.height - y - addRow.height - 12
        clip: true
        spacing: 2
        model: screen.s.rows
        boundsBehavior: Flickable.StopAtBounds
        delegate: Rectangle {
            id: row
            required property var modelData
            required property int index
            width: list.width
            height: 34
            radius: 6
            color: rowHover.containsMouse ? theme.surface0 : "transparent"
            MouseArea { id: rowHover; anchors.fill: parent; hoverEnabled: true; acceptedButtons: Qt.NoButton }
            Row {
                x: 8
                anchors.verticalCenter: parent.verticalCenter
                Text {
                    width: screen.nameW
                    anchors.verticalCenter: parent.verticalCenter
                    text: row.modelData.name
                    color: theme.text
                    font.pixelSize: 13
                    elide: Text.ElideRight
                }
                Repeater {
                    model: row.modelData.cells
                    delegate: Item {
                        required property string modelData
                        required property int index
                        width: screen.colW
                        height: 26
                        Rectangle {
                            anchors.centerIn: parent
                            width: 44
                            height: 24
                            radius: 12
                            color: modelData === "on" ? theme.green : (cellArea.containsMouse ? theme.surface1 : theme.surface0)
                            border.color: row.modelData.differs[index] ? theme.peach : "transparent"
                            Text {
                                anchors.centerIn: parent
                                text: (modelData === "on" ? "on" : modelData === "off" ? "off" : "·")
                                      + (row.modelData.differs[index] ? " ≠" : "")
                                color: modelData === "on" ? theme.base : (modelData === "" ? theme.overlay0 : theme.text)
                                font.pixelSize: 11
                            }
                            MouseArea {
                                id: cellArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: screen.act("toggle", row.index, index)
                            }
                        }
                    }
                }
                Text {
                    width: 70
                    anchors.verticalCenter: parent.verticalCenter
                    text: "  " + row.modelData.transport
                    color: theme.subtext
                    font.pixelSize: 12
                }
                Text {
                    width: Math.max(80, row.width - screen.nameW - screen.s.agents.length * screen.colW - 70 - rowButtons.width - 24)
                    anchors.verticalCenter: parent.verticalCenter
                    text: row.modelData.test || ""
                    color: row.modelData.testState === "ok" ? theme.green
                         : row.modelData.testState === "bad" ? theme.red
                         : row.modelData.testState === "auth" ? theme.yellow : theme.overlay0
                    font.pixelSize: 12
                    elide: Text.ElideRight
                }
            }
            Row {
                id: rowButtons
                anchors.right: parent.right
                anchors.rightMargin: 6
                anchors.verticalCenter: parent.verticalCenter
                spacing: 4
                ToolButton { label: "test"; onClicked: screen.act("test", row.index, -1) }
                ToolButton { label: "details"; onClicked: screen.act("detail", row.index, -1) }
                ToolButton { label: "remove"; danger: true; onClicked: screen.act("remove", row.index, -1) }
            }
        }
        Text {
            visible: !screen.s.loading && screen.s.rows.length === 0
            y: 10
            x: 8
            text: "No MCP server in any agent yet: add one below."
            color: theme.overlay0
            font.pixelSize: 13
        }
    }

    // A new server: name and address, or name, KEY=value and a command.
    Row {
        id: addRow
        anchors.bottom: parent.bottom
        width: parent.width
        spacing: 6
        ToolField {
            id: addLine
            width: parent.width - toWhom.width - addButton.width - 12
            hint: "add: notes https://example.com/mcp   ·   fs -- npx -y server-fs ~/docs"
            onAccepted: addButton.clicked()
        }
        ToolButton {
            id: toWhom
            anchors.verticalCenter: parent.verticalCenter
            label: "to " + (screen.addTo < 0 ? "every agent" : screen.s.agents[screen.addTo]) + " ▾"
            // Steps through the agents, every agent first.
            onClicked: screen.addTo = screen.addTo + 1 >= screen.s.agents.length ? -1 : screen.addTo + 1
        }
        ToolButton {
            id: addButton
            anchors.verticalCenter: parent.verticalCenter
            label: "add"
            primary: true
            onClicked: if (addLine.text.trim() !== "") {
                screen.act("add", -1, screen.addTo, addLine.text)
                addLine.text = ""
            }
        }
    }
}
