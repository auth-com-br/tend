// The dock's files: the TUI's files panel (explorer.Panel) natively — the
// tree with git's letters, search, and the changes with stage, discard and
// commit. A file opens in a document tab; a change opens its diff, side by
// side, where a line's comment goes to the tab's agent.
import QtQuick
import Tend 1.0

Item {
    id: files
    readonly property var f: (st.dock && st.dock.files) || ({ view: "tree", repos: [] })
    function call(args) { Bridge.call("files", args) }
    function letterColor(l) {
        switch (l) {
        case "M": return theme.yellow
        case "A": return theme.green
        case "D": return theme.red
        case "?": case "U": return theme.teal
        case "!": return theme.red
        }
        return theme.subtext
    }

    // Header: the repository, its branch, and how far it is from upstream.
    Column {
        id: head
        width: parent.width
        spacing: 8
        Repeater {
            model: files.f.repos || []
            delegate: Row {
                required property var modelData
                spacing: 6
                width: head.width
                Text { text: modelData.jj ? "◇" : "⎇"; color: theme.accent; font.pixelSize: 13 }
                Text { text: modelData.name || "."; color: theme.text; font.pixelSize: 13; font.bold: true }
                Text { text: modelData.branch; color: theme.peach; font.pixelSize: 12 }
                Text {
                    text: (modelData.ahead ? "↑" + modelData.ahead + " " : "") + (modelData.behind ? "↓" + modelData.behind : "")
                    color: theme.peach
                    font.pixelSize: 12
                }
                Text {
                    text: "⟳"
                    color: syncArea.containsMouse ? theme.accent : theme.subtext
                    font.pixelSize: 13
                    MouseArea { id: syncArea; anchors.fill: parent; anchors.margins: -4; hoverEnabled: true; onClicked: files.call({ do: "sync", repo: modelData.top }) }
                }
            }
        }
        Row {
            spacing: 4
            Repeater {
                model: [{ v: "tree", l: "files" }, { v: "search", l: "search" }, { v: "changes", l: "changes" }]
                delegate: Rectangle {
                    required property var modelData
                    readonly property bool chosen: files.f.view === modelData.v
                    height: 26
                    width: viewLabel.width + 18
                    radius: 6
                    color: chosen ? theme.surface0 : "transparent"
                    Text { id: viewLabel; anchors.centerIn: parent; text: modelData.l; color: chosen ? theme.text : theme.subtext; font.pixelSize: 12 }
                    MouseArea { anchors.fill: parent; onClicked: files.call({ do: "view", view: modelData.v }) }
                }
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                leftPadding: 8
                text: files.f.busy ? "working…" : ""
                color: theme.overlay0
                font.pixelSize: 11
            }
        }
    }

    // The tree.
    ListView {
        id: tree
        visible: files.f.view === "tree"
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        model: files.f.entries || []
        boundsBehavior: Flickable.StopAtBounds
        delegate: Rectangle {
            required property var modelData
            width: tree.width
            height: 24
            radius: 4
            color: rowArea.containsMouse ? theme.surface0 : "transparent"
            Row {
                x: 4 + modelData.depth * 14
                anchors.verticalCenter: parent.verticalCenter
                spacing: 5
                Text {
                    width: 10
                    text: modelData.dir ? (modelData.expanded ? "▾" : "▸") : ""
                    color: theme.overlay0
                    font.pixelSize: 11
                }
                Text {
                    text: modelData.name
                    color: modelData.ignored ? theme.overlay0 : (modelData.status ? files.letterColor(modelData.status) : theme.text)
                    font.pixelSize: 13
                }
            }
            Text {
                anchors.right: parent.right
                anchors.rightMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                visible: !!modelData.status
                text: modelData.status === "!" ? "●" : (modelData.status || "")
                color: files.letterColor(modelData.status)
                font.pixelSize: 12
                font.bold: true
            }
            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                onClicked: {
                    if (modelData.dir) files.call({ do: "toggle", rel: modelData.rel, open: !modelData.expanded })
                    else files.call({ do: "open", path: modelData.path })
                }
            }
        }
    }

    // Search: text in the project's files (git grep), a click opening the
    // file at the line.
    Item {
        visible: files.f.view === "search"
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: parent.bottom
        width: parent.width
        Rectangle {
            id: searchBox
            width: parent.width
            height: 30
            radius: 6
            color: theme.base
            border.color: searchInput.activeFocus ? theme.accent : theme.surface1
            TextInput {
                id: searchInput
                anchors.fill: parent
                anchors.leftMargin: 8
                anchors.rightMargin: 8
                verticalAlignment: TextInput.AlignVCenter
                color: theme.text
                font.pixelSize: 13
                text: files.f.query || ""
                selectByMouse: true
                onAccepted: files.call({ do: "search", query: text })
            }
        }
        ListView {
            id: hits
            anchors.top: searchBox.bottom
            anchors.topMargin: 6
            anchors.bottom: parent.bottom
            width: parent.width
            clip: true
            model: files.f.hits || []
            delegate: Rectangle {
                required property var modelData
                width: hits.width
                height: hitCol.height + 8
                color: hitArea.containsMouse ? theme.surface0 : "transparent"
                radius: 4
                Column {
                    id: hitCol
                    x: 4
                    y: 4
                    width: parent.width - 8
                    Text { text: modelData.path + ":" + modelData.line; color: theme.accent; font.pixelSize: 11 }
                    Text { width: parent.width; text: modelData.text.trim(); color: theme.text; font.pixelSize: 12; font.family: "monospace"; elide: Text.ElideRight }
                }
                MouseArea {
                    id: hitArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: files.call({ do: "open", path: files.f.root + "/" + modelData.path, line: modelData.line })
                }
            }
        }
    }

    // Changes: per repository, staged and the rest, with the commit.
    Flickable {
        visible: files.f.view === "changes"
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        contentHeight: changesCol.height
        Column {
            id: changesCol
            width: parent.width
            spacing: 6
            Repeater {
                model: files.f.changes || []
                delegate: Column {
                    id: repoChanges
                    required property var modelData
                    width: changesCol.width
                    spacing: 4
                    Group { title: "STAGED"; staged: true; repo: repoChanges.modelData.repo; items: repoChanges.modelData.staged }
                    Group { title: "CHANGES"; staged: false; repo: repoChanges.modelData.repo; items: repoChanges.modelData.unstaged.concat(repoChanges.modelData.untracked) }
                    Group { title: "CONFLICTS"; staged: false; repo: repoChanges.modelData.repo; items: repoChanges.modelData.conflicted }
                    // The commit: a message, drafted by an agent if asked.
                    Rectangle {
                        width: parent.width
                        height: 60
                        radius: 6
                        color: theme.base
                        border.color: commitInput.activeFocus ? theme.accent : theme.surface1
                        TextEdit {
                            id: commitInput
                            anchors.fill: parent
                            anchors.margins: 8
                            color: theme.text
                            font.pixelSize: 12
                            wrapMode: TextEdit.Wrap
                            text: files.f.message || ""
                        }
                    }
                    Row {
                        spacing: 6
                        Button { label: "commit"; onClicked: files.call({ do: "commit", repo: repoChanges.modelData.repo, message: commitInput.text }) }
                        Button { label: "✧ draft"; onClicked: files.call({ do: "draft", repo: repoChanges.modelData.repo }) }
                        Button { label: "stage all"; onClicked: files.call({ do: "stage-all", repo: repoChanges.modelData.repo }) }
                        Button { label: "merge"; onClicked: files.call({ do: "plan-finish", repo: repoChanges.modelData.repo, action: "merge" }) }
                        Button { label: "PR"; onClicked: files.call({ do: "plan-finish", repo: repoChanges.modelData.repo, action: "pr" }) }
                    }
                }
            }
        }
    }

    component Group: Column {
        id: group
        property string title
        property bool staged
        property string repo
        property var items: []
        visible: items.length > 0
        width: parent ? parent.width : 0
        Text { text: group.title + " · " + group.items.length; color: theme.overlay0; font.pixelSize: 11; font.bold: true; bottomPadding: 2 }
        Repeater {
            model: group.items
            delegate: Rectangle {
                required property var modelData
                width: group.width
                height: 24
                radius: 4
                color: changeArea.containsMouse ? theme.surface0 : "transparent"
                Text { x: 4; width: 14; anchors.verticalCenter: parent.verticalCenter; text: modelData.status; color: files.letterColor(modelData.status); font.pixelSize: 12; font.bold: true }
                Text {
                    x: 22
                    width: parent.width - 80
                    anchors.verticalCenter: parent.verticalCenter
                    text: modelData.path
                    color: theme.text
                    font.pixelSize: 12
                    elide: Text.ElideLeft
                }
                MouseArea {
                    id: changeArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: files.call({ do: "diff", repo: group.repo, path: modelData.path, staged: group.staged })
                }
                Row {
                    anchors.right: parent.right
                    anchors.rightMargin: 4
                    anchors.verticalCenter: parent.verticalCenter
                    visible: changeArea.containsMouse
                    spacing: 8
                    Text {
                        text: group.staged ? "−" : "+"
                        color: theme.green
                        font.pixelSize: 14
                        MouseArea { anchors.fill: parent; anchors.margins: -4; onClicked: files.call({ do: group.staged ? "unstage" : "stage", repo: group.repo, path: modelData.path }) }
                    }
                    Text {
                        text: "↺"
                        color: theme.red
                        font.pixelSize: 13
                        MouseArea { anchors.fill: parent; anchors.margins: -4; onClicked: files.call({ do: "discard", repo: group.repo, path: modelData.path, staged: group.staged }) }
                    }
                }
            }
        }
    }

    component Button: Rectangle {
        id: btn
        property string label
        signal clicked()
        height: 26
        width: btnText.width + 16
        radius: 6
        color: btnArea.containsMouse ? theme.surface1 : theme.surface0
        Text { id: btnText; anchors.centerIn: parent; text: btn.label; color: theme.text; font.pixelSize: 12 }
        MouseArea { id: btnArea; anchors.fill: parent; hoverEnabled: true; onClicked: btn.clicked() }
    }
}
