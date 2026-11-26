// The dock's errors: the TUI's errors panel natively (core/errors.go) — the
// errors a GlitchTip (or Sentry) server collected, by status and project,
// one opened with its stack, resolved or ignored, and handed to an agent:
// typed into the pane the tab was opened from, put in the context, or
// started in a worktree of the space's project. A server is added in the
// TUI's panel; ⟳ reads the settings again.
import QtQuick
import Tend 1.0

Item {
    id: errs
    readonly property var v: (st.dock && st.dock.errors) || ({ sources: [], scopes: [], statuses: [], errors: [] })
    function call(args) { Bridge.call("errors", args) }
    function levelColor(l) {
        switch (l) {
        case "fatal": case "error": return theme.red
        case "warning": return theme.yellow
        }
        return theme.subtext
    }

    Timer {
        id: searchLater
        interval: 400
        onTriggered: errs.call({ do: "search", query: search.text })
    }

    Column {
        id: head
        width: parent.width
        spacing: 8
        Row {
            width: parent.width
            spacing: 6
            Text { text: "Errors"; color: theme.text; font.pixelSize: 16; font.bold: true }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - 120
                text: [errs.v.server, errs.v.folder ? "for " + errs.v.folder : "", errs.v.busy || errs.v.reading ? "asking…" : ""].filter(Boolean).join(" · ")
                color: theme.subtext
                font.pixelSize: 12
                elide: Text.ElideRight
            }
        }
        Text {
            visible: !errs.v.connected
            width: parent.width
            text: "No error server is kept yet. Add one in tend — prefix+E opens its errors panel, which connects a GlitchTip or Sentry server and keeps its token in the keyring — then ⟳ here reads it."
            color: theme.subtext
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
        Flow {
            width: parent.width
            spacing: 4
            visible: !errs.v.open
            DockChip { label: "⟳"; onClicked: errs.call({ do: "refresh" }) }
            Repeater {
                model: (errs.v.sources || []).length > 1 ? errs.v.sources : []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: errs.v.source === index
                    onClicked: errs.call({ do: "source", index: index })
                }
            }
        }
        Flow {
            visible: errs.v.connected && !errs.v.open
            width: parent.width
            spacing: 4
            Repeater {
                model: errs.v.statuses || []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: errs.v.status === index
                    onClicked: errs.call({ do: "status", index: index })
                }
            }
        }
        Flow {
            visible: errs.v.connected && !errs.v.open && (errs.v.scopes || []).length > 0
            width: parent.width
            spacing: 4
            Repeater {
                model: errs.v.scopes || []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: errs.v.scope === index
                    onClicked: errs.call({ do: "scope", index: index })
                }
            }
        }
        DockField {
            id: search
            visible: errs.v.connected && !errs.v.open
            width: parent.width
            hint: "search: timeout level:error"
            onTextChanged: searchLater.restart()
            onAccepted: { searchLater.stop(); errs.call({ do: "search", query: search.text }) }
        }
    }

    ListView {
        visible: errs.v.connected && !errs.v.open
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: message.top
        width: parent.width
        clip: true
        spacing: 2
        model: errs.v.errors || []
        delegate: Rectangle {
            id: row
            required property var modelData
            width: ListView.view.width
            height: rowCol.height + 10
            radius: 6
            color: rowArea.containsMouse ? theme.surface0 : "transparent"
            Rectangle { x: 6; y: 10; width: 7; height: 7; radius: 4; color: errs.levelColor(row.modelData.level) }
            Column {
                id: rowCol
                x: 20
                y: 5
                width: parent.width - 26
                spacing: 2
                Text {
                    width: parent.width
                    text: "<font color='" + theme.subtext + "'>" + row.modelData.shortId + "</font> " + row.modelData.title
                    textFormat: Text.StyledText
                    color: theme.text
                    font.pixelSize: 13
                    wrapMode: Text.Wrap
                }
                Text {
                    width: parent.width
                    text: [row.modelData.culprit, row.modelData.count + "×", row.modelData.users ? row.modelData.users + " users" : "",
                           row.modelData.last, row.modelData.project].filter(Boolean).join(" · ")
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
                onClicked: errs.call({ do: "open", error: row.modelData.id })
            }
        }
        Text {
            visible: parent.count === 0 && !errs.v.busy
            text: "Nothing here."
            color: theme.overlay0
            font.pixelSize: 12
        }
    }

    // One error, open, with its latest event's stack.
    Flickable {
        visible: !!errs.v.open
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
            readonly property var e: errs.v.open || ({ exceptions: [], tags: [], crumbs: [] })
            DockChip { label: "← back"; onClicked: errs.call({ do: "back" }) }
            Text {
                width: parent.width
                text: col.e.shortId + " " + (col.e.title || "")
                color: theme.text
                font.pixelSize: 15
                font.bold: true
                wrapMode: Text.Wrap
            }
            Text {
                width: parent.width
                text: [col.e.level, col.e.status, col.e.count + " events", col.e.users ? col.e.users + " users" : "",
                       "last " + col.e.last, "first " + col.e.first, col.e.release ? "release " + col.e.release : ""].filter(Boolean).join(" · ")
                color: theme.subtext
                font.pixelSize: 11
                wrapMode: Text.Wrap
            }
            Flow {
                width: parent.width
                spacing: 4
                DockChip { label: "✦ fix in a worktree (" + (errs.v.agent || "claude") + ")"; primary: true; onClicked: errs.call({ do: "worktree" }) }
                DockChip { label: "type into the pane"; onClicked: errs.call({ do: "fix" }) }
                DockChip { label: "to the context"; onClicked: errs.call({ do: "context" }) }
                DockChip { visible: col.e.status !== "resolved"; label: "resolve"; onClicked: errs.call({ do: "set", status: "resolved" }) }
                DockChip { visible: col.e.status !== "ignored"; label: "ignore"; onClicked: errs.call({ do: "set", status: "ignored" }) }
                DockChip { visible: col.e.status !== "unresolved"; label: "unresolve"; onClicked: errs.call({ do: "set", status: "unresolved" }) }
                DockChip { label: "in browser ↗"; onClicked: Qt.openUrlExternally(col.e.url) }
            }
            Text {
                visible: !!col.e.request
                width: parent.width
                text: col.e.request || ""
                color: theme.peach
                font.pixelSize: 12
                font.family: "monospace"
                wrapMode: Text.WrapAnywhere
            }
            Repeater {
                model: col.e.exceptions || []
                delegate: Column {
                    required property var modelData
                    width: col.width
                    spacing: 3
                    Text {
                        width: parent.width
                        text: modelData.type + ": " + modelData.value
                        color: theme.red
                        font.pixelSize: 13
                        font.bold: true
                        wrapMode: Text.Wrap
                    }
                    // The frames, innermost last as GlitchTip gives them;
                    // the project's own bright, the libraries' dim.
                    Repeater {
                        model: modelData.frames || []
                        delegate: Column {
                            required property var modelData
                            width: col.width
                            Text {
                                width: parent.width
                                text: modelData.file + ":" + modelData.line + "  " + modelData.function
                                color: modelData.inApp ? theme.text : theme.overlay0
                                font.pixelSize: 11
                                font.family: "monospace"
                                wrapMode: Text.WrapAnywhere
                            }
                            Text {
                                visible: !!modelData.code
                                width: parent.width
                                leftPadding: 16
                                text: modelData.code || ""
                                color: theme.yellow
                                font.pixelSize: 11
                                font.family: "monospace"
                                wrapMode: Text.WrapAnywhere
                            }
                        }
                    }
                }
            }
            Text {
                visible: (col.e.tags || []).length > 0
                width: parent.width
                text: (col.e.tags || []).join("  ")
                color: theme.subtext
                font.pixelSize: 11
                wrapMode: Text.Wrap
            }
            Text { visible: (col.e.crumbs || []).length > 0; text: "BREADCRUMBS"; color: theme.overlay0; font.pixelSize: 11; font.bold: true; font.letterSpacing: 1 }
            Repeater {
                model: col.e.crumbs || []
                delegate: Text {
                    required property var modelData
                    width: col.width
                    text: modelData
                    color: theme.subtext
                    font.pixelSize: 11
                    font.family: "monospace"
                    wrapMode: Text.WrapAnywhere
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
        text: errs.v.message || ""
        color: theme.subtext
        font.pixelSize: 11
        wrapMode: Text.Wrap
    }
}
