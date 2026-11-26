// The dock's tickets: the TUI's tickets panel natively (core/tickets.go) —
// Linear, Jira, ClickUp and the Issue Gateway, by preset and scope, one
// opened with its comments, commented on, marked done, and handed to an
// agent: started in a worktree of the space's project, or typed into the
// pane the tab was opened from. An account is added in the TUI's panel;
// ⟳ reads the settings again.
import QtQuick
import Tend 1.0

Item {
    id: tk
    readonly property var v: (st.dock && st.dock.tickets) || ({ accounts: [], scopes: [], filters: [], tickets: [] })
    property bool commenting: false
    function call(args) { Bridge.call("tickets", args) }

    Timer {
        id: searchLater
        interval: 400
        onTriggered: tk.call({ do: "search", query: search.text })
    }

    Column {
        id: head
        width: parent.width
        spacing: 8
        Row {
            width: parent.width
            spacing: 6
            Text { text: "Tickets"; color: theme.text; font.pixelSize: 16; font.bold: true }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - 120
                text: [tk.v.account, tk.v.folder ? "for " + tk.v.folder : "", tk.v.busy || tk.v.reading ? "asking…" : ""].filter(Boolean).join(" · ")
                color: theme.subtext
                font.pixelSize: 12
                elide: Text.ElideRight
            }
        }
        Text {
            visible: !tk.v.connected
            width: parent.width
            text: "No tracker account is kept yet. Add one in tend — prefix+T opens its tickets panel, which connects Linear, Jira, ClickUp or the Issue Gateway — then ⟳ here reads it."
            color: theme.subtext
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
        Flow {
            visible: !tk.v.open
            width: parent.width
            spacing: 4
            DockChip { label: "⟳"; onClicked: tk.call({ do: "refresh" }) }
            Repeater {
                model: (tk.v.accounts || []).length > 1 ? tk.v.accounts : []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: tk.v.active === index
                    onClicked: tk.call({ do: "source", index: index })
                }
            }
        }
        Flow {
            visible: tk.v.connected && !tk.v.open
            width: parent.width
            spacing: 4
            Repeater {
                model: tk.v.filters || []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: tk.v.filter === index
                    onClicked: tk.call({ do: "filter", index: index })
                }
            }
        }
        Flow {
            visible: tk.v.connected && !tk.v.open && (tk.v.scopes || []).length > 0
            width: parent.width
            spacing: 4
            Repeater {
                model: tk.v.scopes || []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: tk.v.scope === index
                    onClicked: tk.call({ do: "scope", index: index })
                }
            }
        }
        DockField {
            id: search
            visible: tk.v.connected && !tk.v.open
            width: parent.width
            hint: "search"
            onTextChanged: searchLater.restart()
            onAccepted: { searchLater.stop(); tk.call({ do: "search", query: search.text }) }
        }
    }

    ListView {
        visible: tk.v.connected && !tk.v.open
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: message.top
        width: parent.width
        clip: true
        spacing: 2
        model: tk.v.tickets || []
        delegate: Rectangle {
            id: row
            required property var modelData
            width: ListView.view.width
            height: rowCol.height + 10
            radius: 6
            color: rowArea.containsMouse ? theme.surface0 : "transparent"
            Column {
                id: rowCol
                x: 6
                y: 5
                width: parent.width - 12
                spacing: 2
                Text {
                    width: parent.width
                    text: "<font color='" + (row.modelData.done ? theme.overlay0 : theme.accent) + "'>" + row.modelData.key + "</font> " + row.modelData.title
                    textFormat: Text.StyledText
                    color: theme.text
                    font.pixelSize: 13
                    wrapMode: Text.Wrap
                }
                Text {
                    width: parent.width
                    text: [row.modelData.state, row.modelData.assignee, row.modelData.scope, row.modelData.age].filter(Boolean).join(" · ")
                    color: theme.subtext
                    font.pixelSize: 11
                    wrapMode: Text.Wrap
                }
            }
            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: tk.call({ do: "open", key: row.modelData.key })
            }
        }
        Text {
            visible: parent.count === 0 && !tk.v.busy
            text: "Nothing here."
            color: theme.overlay0
            font.pixelSize: 12
        }
    }

    Flickable {
        visible: !!tk.v.open
        anchors.top: head.bottom
        anchors.topMargin: 4
        anchors.bottom: message.top
        width: parent.width
        clip: true
        contentHeight: col.height
        Column {
            id: col
            width: parent.width
            spacing: 8
            readonly property var d: tk.v.open || ({ comments: [] })
            DockChip { label: "← back"; onClicked: { tk.commenting = false; tk.call({ do: "back" }) } }
            Text {
                width: parent.width
                text: col.d.key + " " + (col.d.title || "")
                color: theme.text
                font.pixelSize: 15
                font.bold: true
                wrapMode: Text.Wrap
            }
            Text {
                width: parent.width
                text: [col.d.state, col.d.assignee ? "→ " + col.d.assignee : "", col.d.scope, col.d.age].filter(Boolean).join(" · ")
                color: theme.subtext
                font.pixelSize: 11
                wrapMode: Text.Wrap
            }
            Flow {
                width: parent.width
                spacing: 4
                DockChip { label: "✦ work on it (" + (tk.v.agent || "claude") + ")"; primary: true; onClicked: tk.call({ do: "worktree" }) }
                DockChip { label: "type into the pane"; onClicked: tk.call({ do: "fix" }) }
                DockChip { label: tk.commenting ? "cancel" : "comment"; onClicked: tk.commenting = !tk.commenting }
                DockChip { visible: !col.d.done; label: "mark done"; onClicked: tk.call({ do: "done" }) }
                DockChip { visible: !!col.d.url; label: "in browser ↗"; onClicked: Qt.openUrlExternally(col.d.url) }
            }
            Column {
                visible: tk.commenting
                width: parent.width
                spacing: 6
                DockField { id: commentBox; width: parent.width; multiline: true; hint: "a comment; ctrl+enter sends"; onAccepted: sendComment.clicked() }
                DockChip {
                    id: sendComment
                    label: "send"
                    primary: true
                    enabled: commentBox.text.trim() !== ""
                    onClicked: {
                        tk.call({ do: "comment", body: commentBox.text })
                        commentBox.text = ""
                        tk.commenting = false
                    }
                }
            }
            Rectangle { width: parent.width; height: 1; color: theme.surface0 }
            DockMarkdown { source: col.d.body || (tk.v.reading ? "reading…" : "*No description.*") }
            Repeater {
                model: col.d.comments || []
                delegate: Column {
                    required property var modelData
                    width: col.width
                    spacing: 2
                    Rectangle { width: parent.width; height: 1; color: theme.surface0 }
                    Text { text: modelData.author + " · " + modelData.age; color: theme.peach; font.pixelSize: 11 }
                    DockMarkdown { source: modelData.body }
                }
            }
        }
    }

    Text {
        id: message
        anchors.bottom: parent.bottom
        anchors.bottomMargin: (st.dock && st.dock.error) ? 30 : 0
        width: parent.width
        height: text === "" ? 0 : implicitHeight + 6
        text: tk.v.message || ""
        color: theme.subtext
        font.pixelSize: 11
        wrapMode: Text.Wrap
    }
}
