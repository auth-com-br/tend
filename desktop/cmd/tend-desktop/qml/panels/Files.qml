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

    // Header: the folder shown, its repository — or how many, folded, when
    // the folder holds several — and the views. A folder of many
    // repositories listed them all here, a line each, and pushed the tree
    // down: they are one line now, opened when wanted, and each one's
    // branch is on its folder in the tree.
    property bool reposOpen: false
    readonly property var repos: files.f.repos || []
    readonly property var repoByName: {
        var m = {}
        for (var i = 0; i < repos.length; i++) m[repos[i].name || ""] = repos[i]
        return m
    }
    function syncText(r) {
        return (r.ahead ? "↑" + r.ahead : "") + (r.ahead && r.behind ? " " : "") + (r.behind ? "↓" + r.behind : "")
    }

    Column {
        id: head
        width: parent.width
        spacing: 8

        // The folder, and the busy word while git works.
        Item {
            width: parent.width
            height: 22
            Text {
                id: folderName
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width - busy.width - 8
                text: {
                    var r = files.f.root || ""
                    var i = r.lastIndexOf("/")
                    return i >= 0 ? r.slice(i + 1) : r
                }
                color: theme.text
                font.pixelSize: 14
                font.weight: Font.DemiBold
                elide: Text.ElideRight
            }
            Text {
                id: busy
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                text: files.f.busy ? "working…" : ""
                color: theme.overlay0
                font.pixelSize: 11
            }
        }

        // One repository: its branch, how far from upstream, sync.
        Loader {
            width: parent.width
            active: files.repos.length === 1
            visible: active
            sourceComponent: RepoLine { repo: files.repos[0] }
        }

        // Several: one line saying how many, which opens their list.
        Rectangle {
            visible: files.repos.length > 1
            width: parent.width
            height: 26
            radius: 6
            color: reposArea.containsMouse ? theme.surface0 : "transparent"
            Text {
                id: reposChevron
                x: 6
                anchors.verticalCenter: parent.verticalCenter
                text: "›"
                rotation: files.reposOpen ? 90 : 0
                color: theme.overlay0
                font.pixelSize: 15
                Behavior on rotation { NumberAnimation { duration: 110 } }
            }
            Text {
                anchors.left: reposChevron.right
                anchors.leftMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                text: files.repos.length + " repositories"
                color: theme.subtext
                font.pixelSize: 12
            }
            MouseArea {
                id: reposArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: files.reposOpen = !files.reposOpen
            }
        }
        Column {
            visible: files.repos.length > 1 && files.reposOpen
            width: parent.width
            spacing: 2
            Repeater {
                model: files.repos.length > 1 && files.reposOpen ? files.repos : []
                delegate: RepoLine {
                    required property var modelData
                    width: head.width
                    repo: modelData
                    named: true
                }
            }
        }

        // The views, as a segmented control.
        Rectangle {
            width: parent.width
            height: 28
            radius: 7
            color: theme.crust
            Row {
                x: 2
                y: 2
                Repeater {
                    model: [{ v: "tree", l: "Files" }, { v: "search", l: "Search" }, { v: "changes", l: "Changes" }, { v: "history", l: "History" }]
                    delegate: Rectangle {
                        required property var modelData
                        readonly property bool chosen: files.f.view === modelData.v
                        height: 24
                        width: (head.width - 4) / 4
                        radius: 5
                        color: chosen ? theme.surface0 : (viewArea.containsMouse ? Qt.rgba(1, 1, 1, 0.04) : "transparent")
                        Text {
                            anchors.centerIn: parent
                            text: modelData.l
                            color: chosen ? theme.text : theme.subtext
                            font.pixelSize: 12
                            font.weight: chosen ? Font.DemiBold : Font.Normal
                        }
                        MouseArea {
                            id: viewArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: modelData.v === "history" ? files.call({ do: "history", kind: "commits" })
                                                                : files.call({ do: "view", view: modelData.v })
                        }
                    }
                }
            }
        }
    }

    // A repository on a line: (its name,) its branch as a chip — a click
    // opens the branches — how far from upstream, and sync.
    component RepoLine: Item {
        id: rl
        property var repo: ({})
        property bool named: false
        height: 26
        Text {
            id: rlName
            visible: rl.named
            x: 22
            // Measured apart: a Text's own implicitWidth moves with its
            // width when it elides — the loop Qt reported on every start.
            TextMetrics { id: rlNameM; font: rlName.font; text: rlName.text }
            width: visible ? Math.min(rlNameM.advanceWidth + 2, rl.width * 0.5) : 0
            anchors.verticalCenter: parent.verticalCenter
            text: rl.repo.name || "."
            color: theme.text
            font.pixelSize: 12
            elide: Text.ElideMiddle
        }
        BranchChip {
            id: rlChip
            anchors.left: rl.named ? rlName.right : parent.left
            anchors.leftMargin: rl.named ? 8 : 0
            anchors.verticalCenter: parent.verticalCenter
            repo: rl.repo
        }
        Text {
            anchors.left: rlChip.right
            anchors.leftMargin: 6
            anchors.verticalCenter: parent.verticalCenter
            text: files.syncText(rl.repo)
            color: theme.peach
            font.pixelSize: 11
        }
        Rectangle {
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            width: 24
            height: 24
            radius: 6
            color: syncArea.containsMouse ? theme.surface0 : "transparent"
            Text {
                anchors.centerIn: parent
                text: "⟳"
                color: syncArea.containsMouse ? theme.accent : theme.overlay0
                font.pixelSize: 13
            }
            MouseArea {
                id: syncArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: files.call({ do: "sync", repo: rl.repo.top })
            }
        }
    }

    // A branch as a chip: a click opens the branch picker.
    component BranchChip: Rectangle {
        id: bc
        property var repo: ({})
        property bool small: false
        width: bcText.implicitWidth + (small ? 10 : 14)
        height: small ? 16 : 20
        radius: height / 2
        color: bcArea.containsMouse ? Qt.rgba(theme.peach.r, theme.peach.g, theme.peach.b, 0.28)
                                    : Qt.rgba(theme.peach.r, theme.peach.g, theme.peach.b, small ? 0.12 : 0.16)
        Text {
            id: bcText
            anchors.centerIn: parent
            text: (bc.repo.jj ? "◇ " : "⎇ ") + (bc.repo.branch || "detached")
            color: theme.peach
            font.pixelSize: bc.small ? 10 : 11
        }
        MouseArea {
            id: bcArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: files.call({ do: "branches", repo: bc.repo.top })
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
        // A tooltip does not stay on a row that scrolled away under it.
        onMovementStarted: { tipTimer.stop(); files.tipPending = null; files.tipEntry = null }

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
                anchors.right: treeChip.visible ? treeChip.left : statusLetter.left
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
            // A folder that is a repository of its own carries its branch,
            // when the folder shown holds several.
            BranchChip {
                id: treeChip
                readonly property var r: files.repos.length > 1 && entry.modelData.dir === true ? files.repoByName[entry.modelData.rel] : undefined
                visible: !!r
                repo: r || ({})
                small: true
                z: 2 // over the row's click, which would fold the folder
                anchors.right: statusLetter.left
                anchors.rightMargin: 6
                anchors.verticalCenter: parent.verticalCenter
            }
            Text {
                id: statusLetter
                anchors.right: parent.right
                anchors.rightMargin: 10
                anchors.verticalCenter: parent.verticalCenter
                // One letter (or a dot): a fixed width, not the text's own, which
                // a Text measures from its width — a binding loop.
                width: visible ? 14 : 0
                horizontalAlignment: Text.AlignRight
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
                onContainsMouseChanged: {
                    if (containsMouse) files.hover(entry.modelData, entry)
                    else files.unhover(entry.modelData)
                }
                onPressed: files.unhover(entry.modelData)
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

    // A file's or a folder's icon, drawn: a folder with its tab (open, its
    // front leans forward), or a page with its corner folded and its kind
    // across it in its kind's colour — no icon theme to load, the same on
    // every desktop, and Canvas is QtQuick's own, so no module is added to
    // what the package depends on.
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
        width: 18
        height: 18
        opacity: faded ? 0.45 : 1

        Canvas {
            id: shape
            anchors.fill: parent
            renderStrategy: Canvas.Cooperative
            readonly property color tint: fi.dir ? theme.yellow : fi.kind.color
            onTintChanged: requestPaint()
            Connections { target: fi; function onExpandedChanged() { shape.requestPaint() } function onDirChanged() { shape.requestPaint() } }
            onPaint: {
                var g = getContext("2d")
                g.reset()
                var c = tint
                var fill = Qt.rgba(c.r, c.g, c.b, 0.22)
                var solid = Qt.rgba(c.r, c.g, c.b, 0.9)
                g.lineJoin = "round"
                g.lineWidth = 1.2
                if (fi.dir) {
                    // The back, with its tab.
                    g.beginPath()
                    g.moveTo(1.5, 4.5)
                    g.lineTo(6.5, 4.5)
                    g.lineTo(8, 6)
                    g.lineTo(16, 6)
                    g.quadraticCurveTo(16.5, 6, 16.5, 6.5)
                    g.lineTo(16.5, 14.5)
                    g.quadraticCurveTo(16.5, 15, 16, 15)
                    g.lineTo(2, 15)
                    g.quadraticCurveTo(1.5, 15, 1.5, 14.5)
                    g.closePath()
                    g.fillStyle = fi.expanded ? fill : Qt.rgba(c.r, c.g, c.b, 0.55)
                    g.strokeStyle = solid
                    g.fill()
                    g.stroke()
                    if (fi.expanded) {
                        // The front, leaning forward: the folder is open.
                        g.beginPath()
                        g.moveTo(3.5, 8.5)
                        g.lineTo(17.5, 8.5)
                        g.lineTo(15.8, 15)
                        g.lineTo(1.6, 15)
                        g.closePath()
                        g.fillStyle = Qt.rgba(c.r, c.g, c.b, 0.75)
                        g.fill()
                        g.stroke()
                    }
                } else {
                    // A page, its top right corner folded.
                    g.beginPath()
                    g.moveTo(3.5, 1.5)
                    g.lineTo(10.5, 1.5)
                    g.lineTo(14.5, 5.5)
                    g.lineTo(14.5, 16.5)
                    g.lineTo(3.5, 16.5)
                    g.closePath()
                    g.fillStyle = fill
                    g.strokeStyle = solid
                    g.fill()
                    g.stroke()
                    g.beginPath()
                    g.moveTo(10.5, 1.5)
                    g.lineTo(10.5, 5.5)
                    g.lineTo(14.5, 5.5)
                    g.stroke()
                }
            }
        }
        // The kind, as a tag across the page.
        Rectangle {
            visible: !fi.dir && fi.kind.label !== ""
            x: Math.round(9 - width / 2)
            y: 9
            width: Math.max(12, tag.implicitWidth + 4)
            height: 7
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

    // What an entry is, said when the pointer rests on it: its name in full,
    // where it is, what git says of it, and for a repository its branch.
    // Drawn here rather than QtQuick.Controls' ToolTip, which the package
    // does not depend on.
    property var tipEntry: null
    property real tipY: 0
    function hover(d, item) {
        tipEntry = null
        tipY = item.mapToItem(files, 0, item.height / 2).y
        tipPending = d
        tipTimer.restart()
    }
    function unhover(d) {
        if (tipPending === d || tipEntry === d) { tipTimer.stop(); tipPending = null; tipEntry = null }
    }
    property var tipPending: null
    Timer {
        id: tipTimer
        interval: 550
        onTriggered: files.tipEntry = files.tipPending
    }
    function statusWord(l) {
        switch (l) {
        case "M": return "modified"
        case "A": return "added"
        case "D": return "deleted"
        case "R": return "renamed"
        case "?": return "untracked"
        case "U": return "conflict"
        case "!": return "conflict"
        }
        return ""
    }
    Rectangle {
        id: tipBox
        visible: !!files.tipEntry
        z: 100
        // Beside the panel, not over it: drawn over the list, it hid the
        // entries under the one pointed at. It stands to the panel's left,
        // over the panes, level with the row, its arrow pointing at it.
        x: -width - 24
        y: Math.max(0, Math.min(files.tipY - height / 2, files.height - height))
        // As wide as its widest line, measured apart from the lines, which
        // wrap at the box's width: bound to them, that is a loop.
        TextMetrics { id: tipNameM; font.pixelSize: 12; font.weight: Font.DemiBold; text: tipBox.e.name || "" }
        TextMetrics { id: tipRelM; font.pixelSize: 11; text: "folder · " + (tipBox.e.rel || "") }
        width: Math.min(340, Math.max(200, tipNameM.advanceWidth + 24, tipRelM.advanceWidth + 24))
        height: tipCol.implicitHeight + 14
        radius: 7
        color: theme.crust
        border.width: 1
        border.color: theme.surface1
        opacity: visible ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: 120 } }
        readonly property var e: files.tipEntry || ({})
        // The arrow, pointing right at the row.
        Rectangle {
            width: 10
            height: 10
            rotation: 45
            x: parent.width - 6
            y: Math.max(6, Math.min(parent.height - 16, files.tipY - tipBox.y - 5))
            color: theme.crust
            border.width: 1
            border.color: theme.surface1
            z: -1
        }
        readonly property var repo: e.dir === true ? files.repoByName[e.rel] : undefined
        Column {
            id: tipCol
            x: 10
            y: 7
            width: tipBox.width - 20
            spacing: 3
            Text {
                width: parent.width
                text: tipBox.e.name || ""
                color: theme.text
                font.pixelSize: 12
                font.weight: Font.DemiBold
                wrapMode: Text.WrapAnywhere
            }
            Text {
                width: parent.width
                text: (tipBox.e.dir === true ? "folder · " : "") + (tipBox.e.rel || "")
                color: theme.subtext
                font.pixelSize: 11
                wrapMode: Text.WrapAnywhere
            }
            Text {
                visible: text !== ""
                text: {
                    var parts = []
                    var w = files.statusWord(tipBox.e.status)
                    if (w) parts.push(w)
                    if (tipBox.e.ignored === true) parts.push("ignored by git")
                    return parts.join(" · ")
                }
                color: tipBox.e.status ? files.letterColor(tipBox.e.status) : theme.overlay0
                font.pixelSize: 11
            }
            Text {
                visible: !!tipBox.repo
                text: tipBox.repo ? "repository · ⎇ " + (tipBox.repo.branch || "detached") + (files.syncText(tipBox.repo) ? "  " + files.syncText(tipBox.repo) : "") : ""
                color: theme.peach
                font.pixelSize: 11
            }
            Text {
                text: "click to open · right click for more"
                color: theme.overlay0
                font.pixelSize: 10
            }
        }
    }
}
