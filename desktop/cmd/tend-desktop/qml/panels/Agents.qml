// The agent manager: the agent CLIs and memory tools the server's machine
// has, with their versions, and how the rest install. Installing one runs
// its command in a tab of its own, as the TUI does, where it is watched,
// answered and stopped, and which then goes on as a shell.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: Object.assign({}, st.agentsScreen || {}, { agents: (st.agentsScreen && st.agentsScreen.agents) || [] })
    title: "Agents"
    subtitle: "installed here, and how the others install"
    message: s.message || ""
    loading: !!s.loading
    boxWidth: 820
    onClosed: Bridge.call("panel", { name: "" })

    Row {
        id: tools
        width: parent.width
        ToolButton { label: "reload"; onClicked: Bridge.call("agents", { do: "reload" }) }
    }

    ListView {
        id: list
        y: tools.height + 10
        width: parent.width
        height: parent.height - y
        clip: true
        spacing: 2
        model: screen.s.agents
        boundsBehavior: Flickable.StopAtBounds
        delegate: Item {
            id: item
            required property var modelData
            required property int index
            // The memory tools come after the agents, under their own
            // heading, as the TUI lists them.
            readonly property bool heading: index === 0 || screen.s.agents[index - 1].memory !== modelData.memory
            width: list.width
            height: row.height + (heading ? 28 : 0)
            Text {
                visible: item.heading
                height: 28
                verticalAlignment: Text.AlignVCenter
                text: item.modelData.memory ? "MEMORY TOOLS" : "AGENTS"
                color: theme.overlay0
                font.pixelSize: 11
                font.bold: true
            }
            Rectangle {
                id: row
                readonly property var modelData: item.modelData
                readonly property bool asking: screen.s.confirm === modelData.id
                y: item.heading ? 28 : 0
                width: list.width
                height: asking ? 64 : 40
                radius: 6
                color: rowArea.containsMouse || asking ? theme.surface0 : "transparent"
                MouseArea {
                    id: rowArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: Bridge.call("agents", { do: "choose", id: row.modelData.id })
                }
                Text {
                    x: 10
                    y: 11
                    text: row.modelData.installed ? "●" : "○"
                    color: row.modelData.installed ? theme.green : theme.overlay0
                    font.pixelSize: 13
                }
                Text {
                    id: nameText
                    x: 30
                    y: 10
                    width: 180
                    text: row.modelData.name
                    color: theme.text
                    font.pixelSize: 14
                    elide: Text.ElideRight
                }
                Text {
                    x: 220
                    y: 12
                    width: row.width - 220 - 12
                    text: row.modelData.installed
                          ? (row.modelData.version || "installed") + "   " + (row.modelData.path || "")
                            + (row.modelData.running ? "   · running" : "")
                          : (row.modelData.install ? "not installed — click to install" : "needs " + (row.modelData.missing || "something this machine has not"))
                    color: row.modelData.installed ? theme.subtext : theme.overlay0
                    font.pixelSize: 12
                    elide: Text.ElideMiddle
                }
                // Asked before it runs: the install is the vendor's command,
                // run on the server's machine.
                Row {
                    visible: row.asking
                    x: 30
                    y: 36
                    spacing: 8
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        width: row.width - 30 - 170
                        text: "install with: " + row.modelData.install
                        color: theme.text
                        font.family: "monospace"
                        font.pixelSize: 11
                        elide: Text.ElideRight
                    }
                    ToolButton { label: "install"; primary: true; onClicked: Bridge.call("agents", { do: "install", id: row.modelData.id }) }
                    ToolButton { label: "cancel"; onClicked: Bridge.call("agents", { do: "cancel" }) }
                }
            }
        }
    }
}
