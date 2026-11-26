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
    // A file's kind: the colour and the short tag its icon carries.
    function kindOf(ext) {
        switch (ext) {
        case "go": case "mod": case "sum": case "go.mod": case "go.sum": return { color: theme.teal, label: "GO" }
        case "rs": return { color: theme.peach, label: "RS" }
        case "js": case "mjs": case "cjs": return { color: theme.yellow, label: "JS" }
        case "jsx": return { color: theme.yellow, label: "JSX" }
        case "ts": return { color: theme.accent, label: "TS" }
        case "tsx": return { color: theme.accent, label: "TSX" }
        case "py": return { color: theme.accent, label: "PY" }
        case "rb": return { color: theme.red, label: "RB" }
        case "java": case "kt": return { color: theme.peach, label: ext.toUpperCase() }
        case "c": case "h": return { color: theme.accent, label: "C" }
        case "cpp": case "cc": case "hpp": return { color: theme.accent, label: "C++" }
        case "php": return { color: theme.accent, label: "PHP" }
        case "html": case "htm": return { color: theme.peach, label: "HTM" }
        case "css": case "scss": return { color: theme.accent, label: "CSS" }
        case "qml": return { color: theme.green, label: "QML" }
        case "json": return { color: theme.yellow, label: "{ }" }
        case "yaml": case "yml": case "toml": return { color: theme.red, label: ext === "toml" ? "TML" : "YML" }
        case "md": case "markdown": return { color: theme.subtext, label: "MD" }
        case "sh": case "bash": case "zsh": return { color: theme.green, label: "SH" }
        case "sql": return { color: theme.yellow, label: "SQL" }
        case "png": case "jpg": case "jpeg": case "gif": case "svg": case "webp": case "ico": return { color: theme.teal, label: "IMG" }
        case "makefile": return { color: theme.peach, label: "MK" }
        case "dockerfile": return { color: theme.accent, label: "DKR" }
        case "lock": return { color: theme.overlay0, label: "LCK" }
        }
        return { color: theme.subtext, label: "" }
    }
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
                // The branch, a click away from the others.
                Text {
                    text: modelData.branch + " ▾"
                    color: brArea.containsMouse ? theme.text : theme.peach
                    font.pixelSize: 12
                    MouseArea { id: brArea; anchors.fill: parent; anchors.margins: -3; hoverEnabled: true; onClicked: files.call({ do: "branches", repo: modelData.top }) }
                }
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
                model: [{ v: "tree", l: "files" }, { v: "search", l: "search" }, { v: "changes", l: "changes" }, { v: "history", l: "history" }]
                delegate: Rectangle {
                    required property var modelData
                    readonly property bool chosen: files.f.view === modelData.v
                    height: 26
                    width: viewLabel.width + 18
                    radius: 6
                    color: chosen ? theme.surface0 : "transparent"
                    Text { id: viewLabel; anchors.centerIn: parent; text: modelData.l; color: chosen ? theme.text : theme.subtext; font.pixelSize: 12 }
                    MouseArea {
                        anchors.fill: parent
                        onClicked: modelData.v === "history" ? files.call({ do: "history", kind: "commits" })
                                                            : files.call({ do: "view", view: modelData.v })
                    }
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

    // The tree, as a file manager's: a folder or a file icon by its kind,
    // guides down each level, a chevron that turns, git's state as the
    // name's colour and a letter, the file open in a document tab marked.
    // A right click is the TUI's tree menu (core/filemenu.go), on an entry
    // or, below them, on the folder shown.
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
        readonly property string openPath: (st.doc && st.doc.kind === "file" && st.doc.path) || ""

        // Below the entries: the folder shown's own menu.
        MouseArea {
            parent: tree
            anchors.fill: parent
            z: -1
            acceptedButtons: Qt.RightButton
            onClicked: (m) => {
                var p = mapToItem(root.contentItem, m.x, m.y)
                Bridge.call("files.menu", { path: "", rel: "", dir: true, x: Math.round(p.x), y: Math.round(p.y) })
            }
        }

        delegate: Item {
            id: entry
            required property var modelData
            readonly property bool open: !modelData.dir && modelData.path === tree.openPath
            width: tree.width
            height: 26

            Rectangle {
                anchors.fill: parent
                radius: 5
                color: entry.open ? Qt.rgba(theme.accent.r, theme.accent.g, theme.accent.b, 0.16)
                     : rowArea.containsMouse ? theme.surface0 : "transparent"
            }
            // A guide down each level the entry is under.
            Repeater {
                model: entry.modelData.depth
                delegate: Rectangle {
                    required property int index
                    x: 11 + index * 16
                    width: 1
                    height: entry.height
                    color: theme.surface0
                }
            }
            Text {
                id: chevron
                x: 4 + entry.modelData.depth * 16
                width: 14
                anchors.verticalCenter: parent.verticalCenter
                horizontalAlignment: Text.AlignHCenter
                visible: entry.modelData.dir === true
                text: "›"
                rotation: entry.modelData.expanded === true ? 90 : 0
                color: theme.overlay0
                font.pixelSize: 15
                Behavior on rotation { NumberAnimation { duration: 110 } }
            }
            FileIcon {
                id: icon
                x: 20 + entry.modelData.depth * 16
                anchors.verticalCenter: parent.verticalCenter
                name: entry.modelData.name
                dir: entry.modelData.dir === true
                expanded: entry.modelData.expanded === true
                faded: entry.modelData.ignored === true
            }
            Text {
                anchors.left: icon.right
                anchors.leftMargin: 7
                anchors.right: statusLetter.left
                anchors.rightMargin: 6
                anchors.verticalCenter: parent.verticalCenter
                text: entry.modelData.name
                elide: Text.ElideMiddle
                font.pixelSize: 13
                font.italic: entry.modelData.ignored === true
                font.weight: entry.open ? Font.DemiBold : Font.Normal
                color: entry.modelData.ignored ? theme.overlay0
                     : entry.modelData.status ? files.letterColor(entry.modelData.status) : theme.text
            }
            Text {
                id: statusLetter
                anchors.right: parent.right
                anchors.rightMargin: 10
                anchors.verticalCenter: parent.verticalCenter
                width: visible ? implicitWidth : 0
                visible: !!entry.modelData.status
                text: entry.modelData.status === "!" ? "●" : (entry.modelData.status === "?" ? "U" : (entry.modelData.status || ""))
                color: files.letterColor(entry.modelData.status)
                font.pixelSize: 11
                font.weight: Font.DemiBold
            }
            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                acceptedButtons: Qt.LeftButton | Qt.RightButton
                onClicked: (m) => {
                    var d = entry.modelData
                    if (m.button === Qt.RightButton) {
                        var p = mapToItem(root.contentItem, m.x, m.y)
                        Bridge.call("files.menu", { path: d.path, rel: d.rel, dir: d.dir === true, x: Math.round(p.x), y: Math.round(p.y) })
                    } else if (d.dir) files.call({ do: "toggle", rel: d.rel, open: !d.expanded })
                    else files.call({ do: "open", path: d.path })
                }
            }
        }
    }

    // A file's or a folder's icon, drawn: a folder (open or shut), or a
    // page with its kind on it in its kind's colour — no icon theme to
    // load, and the same on every desktop.
    component FileIcon: Item {
        id: fi
        property string name: ""
        property bool dir: false
        property bool expanded: false
        property bool faded: false
        readonly property string ext: {
            var n = name.toLowerCase()
            var i = n.lastIndexOf(".")
            if (n === "makefile" || n === "dockerfile" || n === "license" || n === "go.mod" || n === "go.sum") return n
            return i > 0 ? n.slice(i + 1) : ""
        }
        readonly property var kind: files.kindOf(ext)
        width: 16
        height: 16
        opacity: faded ? 0.5 : 1

        // A folder: its tab and its body; open, the body leans forward.
        Rectangle {
            visible: fi.dir
            x: 1; y: 2
            width: 7; height: 4
            radius: 1.5
            color: theme.yellow
            opacity: 0.85
        }
        Rectangle {
            visible: fi.dir
            x: 1; y: 4
            width: 14; height: 10
            radius: 2
            color: Qt.rgba(theme.yellow.r, theme.yellow.g, theme.yellow.b, fi.expanded ? 0.55 : 0.8)
            border.width: 1
            border.color: theme.yellow
        }
        Rectangle {
            visible: fi.dir && fi.expanded
            x: 3; y: 7
            width: 12; height: 7
            radius: 1.5
            color: theme.yellow
            opacity: 0.9
        }

        // A file: a page, its kind in a band across it.
        Rectangle {
            visible: !fi.dir
            x: 2; y: 0
            width: 12; height: 16
            radius: 2
            color: Qt.rgba(fi.kind.color.r, fi.kind.color.g, fi.kind.color.b, 0.16)
            border.width: 1
            border.color: Qt.rgba(fi.kind.color.r, fi.kind.color.g, fi.kind.color.b, 0.7)
        }
        Rectangle {
            visible: !fi.dir && fi.kind.label !== ""
            anchors.horizontalCenter: parent.horizontalCenter
            y: 8
            width: Math.max(14, tag.implicitWidth + 3)
            height: 8
            radius: 2
            color: fi.kind.color
            Text {
                id: tag
                anchors.centerIn: parent
                text: fi.kind.label
                color: theme.base
                font.pixelSize: 6
                font.bold: true
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


    // History: commits, stashes or tags; a click shows one as a diff, and a
    // stash can be applied or popped.
    Item {
        visible: files.f.view === "history"
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: parent.bottom
        width: parent.width
        Row {
            id: kinds
            spacing: 4
            Repeater {
                model: ["commits", "stashes", "tags"]
                delegate: Rectangle {
                    required property string modelData
                    readonly property bool chosen: files.f.historyKind === modelData
                    height: 22
                    width: kindText.width + 14
                    radius: 5
                    color: chosen ? theme.surface1 : theme.surface0
                    Text { id: kindText; anchors.centerIn: parent; text: modelData; color: theme.text; font.pixelSize: 11 }
                    MouseArea { anchors.fill: parent; onClicked: files.call({ do: "history", kind: modelData }) }
                }
            }
        }
        ListView {
            id: histList
            anchors.top: kinds.bottom
            anchors.topMargin: 6
            anchors.bottom: parent.bottom
            width: parent.width
            clip: true
            model: files.f.history || []
            delegate: Rectangle {
                required property var modelData
                width: histList.width
                height: histCol.height + 8
                radius: 4
                color: histArea.containsMouse ? theme.surface0 : "transparent"
                Column {
                    id: histCol
                    x: 4
                    y: 4
                    width: parent.width - 8
                    Text { width: parent.width; text: modelData.subject; color: theme.text; font.pixelSize: 12; elide: Text.ElideRight }
                    Text { text: modelData.ref.slice(0, 12) + (modelData.meta ? " · " + modelData.meta : ""); color: theme.overlay0; font.pixelSize: 11 }
                }
                MouseArea { id: histArea; anchors.fill: parent; hoverEnabled: true; onClicked: files.call({ do: "show", ref: modelData.ref }) }
                Row {
                    anchors.right: parent.right
                    anchors.rightMargin: 6
                    anchors.verticalCenter: parent.verticalCenter
                    visible: histArea.containsMouse && files.f.historyKind === "stashes"
                    spacing: 6
                    Button { label: "apply"; onClicked: files.call({ do: "stash-apply", ref: modelData.ref }) }
                    Button { label: "pop"; onClicked: files.call({ do: "stash-pop", ref: modelData.ref }) }
                }
            }
        }
    }

    // The branch picker: a remote branch becomes a local one tracking it, as
    // the panel's does.
    Rectangle {
        visible: !!files.f.branchRepo
        anchors.top: head.bottom
        anchors.bottom: parent.bottom
        width: parent.width
        z: 5
        color: theme.mantle
        Text { id: brTitle; text: "switch to"; color: theme.overlay0; font.pixelSize: 11; font.bold: true }
        Text {
            anchors.right: parent.right
            text: "×"
            color: theme.overlay0
            font.pixelSize: 14
            MouseArea { anchors.fill: parent; anchors.margins: -4; onClicked: files.call({ do: "branches-close" }) }
        }
        ListView {
            id: brList
            anchors.top: brTitle.bottom
            anchors.topMargin: 6
            anchors.bottom: parent.bottom
            width: parent.width
            clip: true
            model: files.f.branches || []
            delegate: Rectangle {
                required property var modelData
                width: brList.width
                height: 26
                radius: 4
                color: bArea.containsMouse ? theme.surface0 : "transparent"
                Text {
                    x: 6
                    anchors.verticalCenter: parent.verticalCenter
                    text: (modelData.current ? "● " : "  ") + modelData.name
                    color: modelData.remote ? theme.subtext : theme.text
                    font.pixelSize: 12
                }
                MouseArea {
                    id: bArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: if (!modelData.current) files.call({ do: "switch", repo: files.f.branchRepo, name: modelData.name, remote: modelData.remote })
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
