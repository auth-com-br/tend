// The desktop window: the sidebar (spaces and agents), the tabs of the space
// shown, its panes laid out as the server lays them out, and a status line.
//
// Everything shown comes from Go as one state (Bridge.state, JSON), and
// every action goes back through Bridge.call: QML decides how things look,
// never what they are (docs/DESKTOP.md, "Go and QML").
import QtQuick
import QtQuick.Window
import Tend 1.0

Window {
    id: root
    visible: true
    width: 1280
    height: 800
    color: theme.base
    title: "tend — " + (st.session || "")

    // catppuccin mocha, tend's default theme's family, until the desktop
    // reads the theme from the settings (phase 2).
    // The theme the settings name, resolved in Go as the TUI resolves it
    // (ui.PaletteFor); catppuccin mocha's values until the first state.
    QtObject {
        id: theme
        readonly property var t: st.theme || ({})
        readonly property color base: t.base || "#1e1e2e"
        readonly property color mantle: t.mantle || "#181825"
        readonly property color crust: t.crust || "#11111b"
        readonly property color surface0: t.surface0 || "#313244"
        readonly property color surface1: t.surface1 || "#45475a"
        readonly property color overlay0: t.overlay0 || "#6c7086"
        readonly property color subtext: t.subtext || "#a6adc8"
        readonly property color text: t.text || "#cdd6f4"
        readonly property color accent: t.accent || "#89b4fa"
        readonly property color green: t.green || "#a6e3a1"
        readonly property color yellow: t.yellow || "#f9e2af"
        readonly property color red: t.red || "#f38ba8"
        readonly property color teal: t.teal || "#94e2d5"
        readonly property color peach: t.peach || "#fab387"

        // The TUI's state colours (internal/ui Theme.StateStyle): working
        // yellow, blocked red, idle green, done cyan, the rest faint.
        function stateColor(state, running) {
            if (running === false) return overlay0
            switch (state) {
            case "working": return yellow
            case "blocked": return red
            case "idle": return green
            case "done": return teal
            }
            return overlay0
        }
        function stateMark(state) {
            switch (state) {
            case "working": return "●"
            case "blocked": return "▲"
            case "idle": return "○"
            case "done": return "✓"
            case "asleep": return "◌"
            }
            return "·"
        }
    }

    property var st: ({})

    // Whether the window is the one in front: the pane in view is not
    // announced while it is, and the server learns from it when an agent
    // finished unseen.
    onActiveChanged: Bridge.call("window.focus", { focused: active })

    // Whether the desktop is light or dark, which a theme with auto_switch
    // follows: Qt knows, and tells as it changes.
    function reportAppearance() { Bridge.call("appearance", { light: Qt.styleHints.colorScheme === Qt.ColorScheme.Light }) }
    Connections {
        target: Qt.styleHints
        function onColorSchemeChanged() { root.reportAppearance() }
    }

    // ---- browser tabs ------------------------------------------------------
    //
    // A browser tab is this window's, not the session's — like a folded
    // group, it is what one person is looking at — and belongs to the space
    // it was opened in. Its page is Browser.qml, the only file importing
    // QtWebEngine, made when the first one opens, so the window has no
    // Chromium in it until then.

    property int activeBrowser: 0
    property int nextBrowser: 1
    property var browsers: ({})

    function openBrowser(url) {
        var component = Qt.createComponent("Browser.qml")
        if (component.status !== Component.Ready) {
            Bridge.call("status", { text: "the browser needs Qt WebEngine: sudo apt install qml6-module-qtwebengine" })
            return
        }
        var page = component.createObject(browserLayer)
        var id = nextBrowser++
        page.anchors.fill = browserLayer
        if (url) page.go(url)
        browsers[id] = page
        browserModel.append({ key: "b" + id, bid: id, workspace: st.workspace, title: "New tab" })
        page.pageTitleChanged.connect(function () { root.setBrowserTitle(id, page.pageTitle) })
        showBrowser(id)
    }
    function showBrowser(id) {
        activeBrowser = id
        for (var k in browsers) browsers[k].visible = (Number(k) === id)
    }
    function closeBrowser(id) {
        for (var i = 0; i < browserModel.count; i++)
            if (browserModel.get(i).bid === id) { browserModel.remove(i); break }
        if (browsers[id]) { browsers[id].destroy(); delete browsers[id] }
        if (activeBrowser === id) showBrowser(0)
    }
    function setBrowserTitle(id, title) {
        for (var i = 0; i < browserModel.count; i++)
            if (browserModel.get(i).bid === id) browserModel.setProperty(i, "title", title || "New tab")
    }
    ListModel { id: browserModel }
    readonly property real cw: Bridge.cellWidth
    readonly property real ch: Bridge.cellHeight

    // syncModel brings a ListModel to an array's order and contents,
    // keeping the rows that stay — so a list does not jump back to its top,
    // and a pane's terminal is not made again, each time the state changes.
    function syncModel(model, arr, keyOf, toRow) {
        var want = {}
        for (var i = 0; i < arr.length; i++) want[keyOf(arr[i])] = true
        for (var r = model.count - 1; r >= 0; r--)
            if (!want[model.get(r).key]) model.remove(r)
        for (var n = 0; n < arr.length; n++) {
            var row = toRow(arr[n])
            row.key = keyOf(arr[n])
            var at = -1
            for (var k = n; k < model.count; k++)
                if (model.get(k).key === row.key) { at = k; break }
            if (at < 0) model.insert(n, row)
            else {
                if (at !== n) model.move(at, n, 1)
                model.set(n, row)
            }
        }
    }

    function sidebarRow(r) {
        return {
            kind: r.kind || "", label: r.label || "", trailing: r.trailing || "",
            rstate: r.state || "", running: r.running === true, active: r.active === true,
            folded: r.folded === true, depth: r.depth || 0, group: r.group || "",
            pane: r.pane || 0, tab: r.tab || 0, workspace: r.workspace || 0,
            lines: JSON.stringify(r.lines || [])
        }
    }

    function applyState() {
        var s = JSON.parse(Bridge.state)
        st = s
        syncModel(spaceModel, s.spaces || [], function (r) { return r.kind + ":" + (r.workspace || 0) + ":" + (r.group || "") }, sidebarRow)
        syncModel(agentModel, s.agents || [], function (r) { return r.kind + ":" + (r.pane || 0) + ":" + (r.tab || 0) }, sidebarRow)
        syncModel(tabModel, s.tabs || [], function (t) { return "t" + t.id }, function (t) {
            return { tabId: t.id, name: t.name || "", active: t.active === true, tstate: t.state || "" }
        })
        syncModel(paneModel, s.panes || [], function (p) { return "p" + p.pane }, function (p) {
            return { pane: p.pane, cx: p.x, cy: p.y, cols: p.cols, rows: p.rows,
                          title: p.title || "", agent: p.agent || "", pstate: p.state || "", focused: p.focused === true,
                     scroll: p.scroll || 0, history: p.history || 0 }
        })
    }

    Connections {
        target: Bridge
        function onStateChanged() { root.applyState() }
    }
    Component.onCompleted: {
        reportAppearance()
        applyState()
        if (Bridge.startUrl !== "") openBrowser(Bridge.startUrl)
    }

    ListModel { id: spaceModel }
    ListModel { id: agentModel }
    ListModel { id: tabModel }
    ListModel { id: paneModel }

    // ---- sidebar ---------------------------------------------------------

    Rectangle {
        id: sidebar
        width: 290
        anchors.top: parent.top
        anchors.bottom: statusBar.top
        anchors.left: parent.left
        color: theme.mantle

        Column {
            anchors.fill: parent
            anchors.topMargin: 8

            SectionHeading {
                text: "SPACES"
                action: "+"
                onActionClicked: Bridge.call("workspace.new", {})
            }
            ListView {
                id: spaceList
                width: parent.width
                height: (parent.height - 80) * 0.45
                clip: true
                model: spaceModel
                boundsBehavior: Flickable.StopAtBounds
                delegate: SidebarEntry {
                    width: spaceList.width
                    onClicked: {
                        if (kind === "group") Bridge.call("group.toggle", { group: group })
                        else if (workspace) Bridge.call("workspace", { workspace: workspace })
                    }
                }
            }

            Item { width: 1; height: 8 }

            SectionHeading {
                text: "AGENTS"
                action: st.grouped ? "grouped" : "flat"
                onActionClicked: Bridge.call("agents.grouped", {})
            }
            ListView {
                id: agentList
                width: parent.width
                height: (parent.height - 80) * 0.55
                clip: true
                model: agentModel
                boundsBehavior: Flickable.StopAtBounds
                delegate: SidebarEntry {
                    width: agentList.width
                    onClicked: if (pane) Bridge.call("jump", { pane: pane })
                }
                Text {
                    visible: agentModel.count === 0
                    anchors.left: parent.left
                    anchors.leftMargin: 14
                    text: "no agents running"
                    color: theme.overlay0
                    font.pixelSize: 12
                }
            }
        }

        // The TUI's sidebar foot: a new space, and the menu of everything
        // else ("● menu" when an update is ready, so the menu that says so is
        // found).
        Row {
            anchors.left: parent.left
            anchors.leftMargin: 8
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 6
            spacing: 4
            ToolButton { width: 64; text: "+ space"; onClicked: Bridge.call("workspace.new", {}) }
            ToolButton {
                id: globalMenuButton
                width: 64
                text: (st.update ? "● " : "") + "menu"
                onClicked: {
                    var p = mapToItem(root.contentItem, 0, -8)
                    Bridge.call("menu.open", { kind: "global", x: Math.round(p.x), y: -1 })
                }
            }
        }

        Rectangle {
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            width: 1
            color: theme.crust
        }
    }

    component SectionHeading: Item {
        id: heading
        property alias text: label.text
        property string action: ""
        signal actionClicked()
        width: parent ? parent.width : 0
        height: 26
        Text {
            id: label
            anchors.left: parent.left
            anchors.leftMargin: 14
            anchors.verticalCenter: parent.verticalCenter
            color: theme.overlay0
            font.pixelSize: 12
            font.bold: true
            font.letterSpacing: 1
        }
        Text {
            visible: heading.action !== ""
            anchors.right: parent.right
            anchors.rightMargin: 12
            anchors.verticalCenter: parent.verticalCenter
            text: heading.action
            color: actionArea.containsMouse ? theme.text : theme.overlay0
            font.pixelSize: 12
            MouseArea {
                id: actionArea
                anchors.fill: parent
                anchors.margins: -6
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: heading.actionClicked()
            }
        }
    }

    // One sidebar row, the TUI's ui.SidebarRow: a space, a group of spaces,
    // an agent, or a tab heading over agents.
    component SidebarEntry: Rectangle {
        id: entry
        required property string kind
        required property string label
        required property string trailing
        required property string rstate
        required property bool running
        required property bool active
        required property bool folded
        required property int depth
        required property string group
        required property int pane
        required property int workspace
        required property int tab
        required property string lines
        signal clicked()

        readonly property var tokenLines: JSON.parse(lines)
        height: kind === "tab" ? 24 : Math.max(28, 8 + 20 * Math.max(1, tokenLines.length))
        color: active ? theme.surface0 : (hover.containsMouse ? Qt.rgba(1, 1, 1, 0.04) : "transparent")
        radius: 4

        Rectangle {
            visible: entry.active
            width: 3
            height: parent.height - 8
            radius: 2
            anchors.left: parent.left
            anchors.leftMargin: 3
            anchors.verticalCenter: parent.verticalCenter
            color: theme.accent
        }

        Row {
            id: lead
            anchors.left: parent.left
            anchors.leftMargin: 14 + entry.depth * 14
            anchors.top: parent.top
            anchors.topMargin: entry.kind === "tab" ? 4 : 5
            spacing: 6

            // A group's fold arrow, or a space's or agent's state mark.
            Text {
                text: entry.kind === "group" ? (entry.folded ? "▸" : "▾")
                    : entry.kind === "tab" ? "" : theme.stateMark(entry.rstate)
                color: entry.kind === "group" ? theme.overlay0 : theme.stateColor(entry.rstate, entry.running)
                font.pixelSize: 12
                width: entry.kind === "tab" ? 0 : 10
            }
            Column {
                spacing: 2
                // A row with tokens is drawn from them, as the settings lay
                // it out; one without (a group, a tab heading) by its label.
                Repeater {
                    model: entry.tokenLines.length > 0 && entry.kind !== "group" ? entry.tokenLines : [[{ kind: "primary", text: entry.label }]]
                    delegate: Row {
                        id: tokenRow
                        required property var modelData
                        required property int index
                        spacing: 6
                        Repeater {
                            model: tokenRow.modelData
                            delegate: Text {
                                required property var modelData
                                visible: modelData.kind !== "stateIcon"
                                text: modelData.text
                                elide: Text.ElideRight
                                font.pixelSize: entry.kind === "tab" ? 12 : (tokenRow.index === 0 ? 14 : 12)
                                font.bold: entry.kind === "group"
                                color: {
                                    switch (modelData.kind) {
                                    case "primary": return entry.kind === "tab" ? theme.overlay0 : theme.text
                                    case "stateText": return theme.stateColor(entry.rstate, entry.running)
                                    case "git": return theme.peach
                                    case "gauge": return modelData.level === 2 ? theme.red : modelData.level === 1 ? theme.yellow : theme.subtext
                                    }
                                    return theme.subtext
                                }
                            }
                        }
                    }
                }
            }
        }
        Text {
            visible: entry.trailing !== ""
            anchors.right: parent.right
            anchors.rightMargin: 12
            anchors.top: parent.top
            anchors.topMargin: 6
            text: entry.trailing
            color: theme.red
            font.pixelSize: 11
        }
        MouseArea {
            id: hover
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            acceptedButtons: Qt.LeftButton | Qt.RightButton
            onClicked: (mouse) => {
                if (mouse.button === Qt.RightButton) {
                    var kinds = { space: "space", group: "group", agent: "agent" }
                    if (!kinds[entry.kind]) return
                    var p = mapToItem(root.contentItem, mouse.x, mouse.y)
                    Bridge.call("menu.open", { kind: kinds[entry.kind], workspace: entry.workspace, pane: entry.pane,
                                               tab: entry.tab, group: entry.group, x: Math.round(p.x), y: Math.round(p.y) })
                } else entry.clicked()
            }
        }
    }

    // ---- tabs ------------------------------------------------------------

    Rectangle {
        id: tabBar
        anchors.left: sidebar.right
        anchors.right: dockSide.left
        anchors.top: parent.top
        height: 38
        color: theme.mantle

        Row {
            anchors.left: parent.left
            anchors.leftMargin: 6
            anchors.verticalCenter: parent.verticalCenter
            spacing: 4
            Repeater {
                model: tabModel
                delegate: Rectangle {
                    id: tabEntry
                    required property int tabId
                    required property string name
                    required property bool active
                    required property string tstate
                    height: 30
                    width: tabRow.width + 24
                    radius: 5
                    color: active && root.activeBrowser === 0 && !st.doc ? theme.surface0 : (tabHover.containsMouse ? Qt.rgba(1, 1, 1, 0.05) : "transparent")
                    Row {
                        id: tabRow
                        anchors.centerIn: parent
                        spacing: 6
                        Text {
                            visible: tabEntry.tstate !== ""
                            text: theme.stateMark(tabEntry.tstate)
                            color: theme.stateColor(tabEntry.tstate, true)
                            font.pixelSize: 11
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: tabEntry.name
                            color: tabEntry.active ? theme.text : theme.subtext
                            font.pixelSize: 14
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: "×"
                            visible: tabHover.containsMouse || tabEntry.active
                            color: closeArea.containsMouse ? theme.red : theme.overlay0
                            font.pixelSize: 13
                            anchors.verticalCenter: parent.verticalCenter
                            MouseArea {
                                id: closeArea
                                anchors.fill: parent
                                anchors.margins: -4
                                hoverEnabled: true
                                z: 1
                                onClicked: Bridge.call("tab.close", { tab: tabEntry.tabId })
                            }
                        }
                    }
                    // Under the row, not over it: on top it took the click
                    // meant for the x, and the tab was shown, never closed.
                    MouseArea {
                        id: tabHover
                        z: -1
                        anchors.fill: parent
                        hoverEnabled: true
                        acceptedButtons: Qt.LeftButton | Qt.MiddleButton | Qt.RightButton
                        onClicked: (mouse) => {
                            if (mouse.button === Qt.RightButton) {
                                var p = mapToItem(root.contentItem, mouse.x, mouse.y)
                                Bridge.call("menu.open", { kind: "tab", tab: tabEntry.tabId, x: Math.round(p.x), y: Math.round(p.y) })
                            } else if (mouse.button === Qt.MiddleButton) Bridge.call("tab.close", { tab: tabEntry.tabId })
                            else { root.showBrowser(0); Bridge.call("doc.show", { index: 0 }); Bridge.call("tab", { tab: tabEntry.tabId }) }
                        }
                    }
                }
            }
            // Document tabs: a file or a diff opened from the files tab.
            Repeater {
                model: st.docs || []
                delegate: Rectangle {
                    id: dtab
                    required property var modelData
                    readonly property bool shown: st.doc && st.doc.id === modelData.id
                    height: 30
                    width: Math.min(220, dtRow.width + 24)
                    radius: 5
                    color: shown ? theme.surface0 : (dtHover.containsMouse ? Qt.rgba(1, 1, 1, 0.05) : "transparent")
                    MouseArea {
                        id: dtHover
                        z: -1
                        anchors.fill: parent
                        hoverEnabled: true
                        acceptedButtons: Qt.LeftButton | Qt.MiddleButton
                        onClicked: (mouse) => {
                            if (mouse.button === Qt.MiddleButton) Bridge.call("doc.close", { index: dtab.modelData.id })
                            else { root.showBrowser(0); Bridge.call("doc.show", { index: dtab.modelData.id }) }
                        }
                    }
                    Row {
                        id: dtRow
                        anchors.centerIn: parent
                        spacing: 6
                        Text {
                            text: dtab.modelData.title
                            width: Math.min(implicitWidth, 170)
                            elide: Text.ElideMiddle
                            color: dtab.shown ? theme.text : theme.subtext
                            font.pixelSize: 14
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: "×"
                            color: dtClose.containsMouse ? theme.red : theme.overlay0
                            font.pixelSize: 13
                            anchors.verticalCenter: parent.verticalCenter
                            MouseArea { id: dtClose; anchors.fill: parent; anchors.margins: -4; hoverEnabled: true; onClicked: Bridge.call("doc.close", { index: dtab.modelData.id }) }
                        }
                    }
                }
            }
            Repeater {
                model: browserModel
                delegate: Rectangle {
                    id: btab
                    required property int bid
                    required property int workspace
                    required property string title
                    visible: workspace === st.workspace
                    height: 30
                    width: visible ? Math.min(220, btRow.width + 24) : 0
                    radius: 5
                    color: root.activeBrowser === bid ? theme.surface0 : (btHover.containsMouse ? Qt.rgba(1, 1, 1, 0.05) : "transparent")
                    MouseArea {
                        id: btHover
                        z: -1
                        anchors.fill: parent
                        hoverEnabled: true
                        acceptedButtons: Qt.LeftButton | Qt.MiddleButton
                        onClicked: (mouse) => {
                            if (mouse.button === Qt.MiddleButton) root.closeBrowser(btab.bid)
                            else { Bridge.call("doc.show", { index: 0 }); root.showBrowser(btab.bid) }
                        }
                    }
                    Row {
                        id: btRow
                        anchors.centerIn: parent
                        spacing: 6
                        Text { text: "◎"; color: theme.accent; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                        Text {
                            text: btab.title
                            width: Math.min(implicitWidth, 160)
                            elide: Text.ElideRight
                            color: root.activeBrowser === btab.bid ? theme.text : theme.subtext
                            font.pixelSize: 14
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: "×"
                            color: btClose.containsMouse ? theme.red : theme.overlay0
                            font.pixelSize: 13
                            anchors.verticalCenter: parent.verticalCenter
                            MouseArea {
                                id: btClose
                                anchors.fill: parent
                                anchors.margins: -4
                                hoverEnabled: true
                                onClicked: root.closeBrowser(btab.bid)
                            }
                        }
                    }
                }
            }
            ToolButton {
                id: newTabButton
                text: "+"
                tip: "new tab"
                onClicked: newMenu.open = !newMenu.open
            }
        }

        Row {
            anchors.right: parent.right
            anchors.rightMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2
            ToolButton { text: "◫"; tip: "split side by side"; onClicked: Bridge.call("pane.split", { direction: "columns" }) }
            ToolButton { text: "⊟"; tip: "split one over the other"; onClicked: Bridge.call("pane.split", { direction: "rows" }) }
            ToolButton { text: "✕"; tip: "close pane"; onClicked: Bridge.call("pane.close", {}) }
        }
    }


    component Divider: MouseArea {
        id: div
        property string side: "right"
        property int pane: 0
        property real start: 0
        property int applied: 0
        z: 5
        hoverEnabled: true
        cursorShape: side === "right" ? Qt.SplitHCursor : Qt.SplitVCursor
        preventStealing: true
        onPressed: (mouse) => {
            var p = mapToItem(root.contentItem, mouse.x, mouse.y)
            start = side === "right" ? p.x : p.y
            applied = 0
        }
        onPositionChanged: (mouse) => {
            if (!pressed) return
            var p = mapToItem(root.contentItem, mouse.x, mouse.y)
            var moved = (side === "right" ? p.x : p.y) - start
            var cells = Math.round(moved / (side === "right" ? root.cw : root.ch))
            if (cells !== applied) {
                Bridge.call("pane.adjust", { pane: pane, side: side, cells: cells - applied })
                applied = cells
            }
        }
        Rectangle {
            anchors.centerIn: parent
            width: div.side === "right" ? 2 : parent.width
            height: div.side === "right" ? parent.height : 2
            color: theme.accent
            opacity: div.containsMouse || div.pressed ? 0.8 : 0
        }
    }

    component ToolButton: Rectangle {
        id: tb
        property alias text: tbLabel.text
        property string tip: ""
        signal clicked()
        width: 28
        height: 26
        radius: 5
        color: tbArea.containsMouse ? theme.surface0 : "transparent"
        Text {
            id: tbLabel
            anchors.centerIn: parent
            color: tbArea.containsMouse ? theme.text : theme.subtext
            font.pixelSize: 14
        }
        MouseArea {
            id: tbArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: tb.clicked()
        }
    }

    // ---- panes -----------------------------------------------------------

    Item {
        id: paneArea
        visible: root.activeBrowser === 0
        anchors.left: sidebar.right
        anchors.right: dockSide.left
        anchors.top: tabBar.bottom
        anchors.bottom: statusBar.top
        anchors.margins: 4

        // The area in cells, which the server lays the tab out in. Sent
        // once the size settles, not for every pixel of a drag.
        function reportArea() { areaTimer.restart() }
        onWidthChanged: reportArea()
        onHeightChanged: reportArea()
        Timer {
            id: areaTimer
            interval: 30
            onTriggered: Bridge.call("area", {
                cols: Math.max(1, Math.floor(paneArea.width / root.cw)),
                rows: Math.max(1, Math.floor(paneArea.height / root.ch))
            })
        }

        Text {
            visible: paneModel.count === 0
            anchors.centerIn: parent
            text: "this space has no tabs — + opens one"
            color: theme.overlay0
        }

        Repeater {
            model: paneModel
            delegate: Rectangle {
                id: box
                required property int pane
                required property int cx
                required property int cy
                required property int cols
                required property int rows
                required property bool focused
                required property int scroll
                required property int history
                // The server's rectangles tile the area exactly; each pane
                // keeps a pixel of it as its edge, which shows the focus.
                x: box.cx * root.cw
                y: box.cy * root.ch
                width: box.cols * root.cw
                height: box.rows * root.ch
                color: theme.base
                border.width: 1
                border.color: box.focused && paneModel.count > 1 ? theme.accent : theme.surface0
                radius: 3

                Terminal {
                    id: term
                    anchors.fill: parent
                    anchors.margins: 3
                    pane: box.pane
                    onActivated: Bridge.call("focus", { pane: box.pane })
                }
                // The borders shared with a neighbour are dividers: dragged,
                // they move by whole cells through pane.adjust, as in the TUI.
                // An outside border has nothing beyond it to take space from.
                Divider {
                    visible: box.cx + box.cols < st.areaCols
                    side: "right"
                    pane: box.pane
                    anchors.right: parent.right
                    anchors.rightMargin: -4
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    width: 8
                }
                Divider {
                    visible: box.cy + box.rows < st.areaRows
                    side: "down"
                    pane: box.pane
                    anchors.bottom: parent.bottom
                    anchors.bottomMargin: -4
                    anchors.left: parent.left
                    anchors.right: parent.right
                    height: 8
                }
                // Looking back through the history: how far, of how much. Typing
                // returns to the present.
                Rectangle {
                    visible: box.scroll > 0
                    anchors.top: parent.top
                    anchors.right: parent.right
                    anchors.margins: 6
                    width: scrollLabel.width + 16
                    height: 20
                    radius: 4
                    color: theme.surface1
                    Text {
                        id: scrollLabel
                        anchors.centerIn: parent
                        text: "↑ " + box.scroll + " / " + box.history
                        color: theme.text
                        font.pixelSize: 11
                    }
                }
                onFocusedChanged: if (focused) term.forceActiveFocus()
                Component.onCompleted: if (focused) term.forceActiveFocus()
            }
        }
    }


    // ---- the new-tab menu -------------------------------------------------
    //
    // What a new tab runs: a terminal, an agent installed on the server's
    // machine (the agent manager's catalog), or a web page.

    MouseArea {
        // A press anywhere else puts the menu away.
        anchors.fill: parent
        visible: newMenu.open
        z: 90
        onPressed: newMenu.open = false
    }

    Rectangle {
        id: newMenu
        objectName: "newMenu"
        property bool open: false
        visible: open
        z: 100
        x: tabBar.x + newTabButton.mapToItem(tabBar, 0, 0).x
        y: tabBar.y + tabBar.height - 2
        width: 230
        height: menuColumn.height + 12
        radius: 8
        color: theme.mantle
        border.color: theme.surface1
        border.width: 1

        Column {
            id: menuColumn
            x: 6
            y: 6
            width: parent.width - 12

            MenuItem {
                icon: "❯_"
                text: "Terminal"
                onActivated: { newMenu.open = false; Bridge.call("tab.new", {}) }
            }
            MenuSeparator { visible: (st.newAgents || []).length > 0 }
            MenuLabel { visible: (st.newAgents || []).length > 0; text: "AGENT" }
            Repeater {
                model: st.newAgents || []
                delegate: MenuItem {
                    required property var modelData
                    icon: "✦"
                    text: modelData.name
                    onActivated: { newMenu.open = false; Bridge.call("tab.agent", { agent: modelData.id }) }
                }
            }
            MenuSeparator {}
            MenuItem {
                icon: "◎"
                text: "Browser"
                onActivated: { newMenu.open = false; root.openBrowser("") }
            }
        }
    }

    component MenuItem: Rectangle {
        id: mi
        property string icon: ""
        property alias text: miLabel.text
        signal activated()
        width: parent ? parent.width : 0
        height: 32
        radius: 5
        color: miArea.containsMouse ? theme.surface0 : "transparent"
        Text {
            x: 10
            anchors.verticalCenter: parent.verticalCenter
            width: 22
            text: mi.icon
            color: theme.accent
            font.pixelSize: 13
        }
        Text {
            id: miLabel
            x: mi.icon === "" ? 12 : 38
            anchors.verticalCenter: parent.verticalCenter
            color: theme.text
            font.pixelSize: 14
        }
        MouseArea {
            id: miArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: mi.activated()
        }
    }
    component MenuSeparator: Rectangle {
        width: parent ? parent.width : 0
        height: 9
        color: "transparent"
        Rectangle { anchors.centerIn: parent; width: parent.width - 8; height: 1; color: theme.surface0 }
    }
    component MenuLabel: Text {
        leftPadding: 10
        topPadding: 2
        bottomPadding: 4
        color: theme.overlay0
        font.pixelSize: 11
        font.bold: true
        font.letterSpacing: 1
    }

    Loader {
        anchors.fill: paneArea
        active: !!st.doc && root.activeBrowser === 0
        source: "DocView.qml"
        z: 2
    }

    Item {
        id: browserLayer
        anchors.fill: paneArea
        visible: root.activeBrowser !== 0
    }

    // A browser tab belongs to its space: going to another space shows
    // that space's terminals.
    Connections {
        target: root
        function onStChanged() {
            var b = root.activeBrowser
            if (b === 0) return
            for (var i = 0; i < browserModel.count; i++)
                if (browserModel.get(i).bid === b && browserModel.get(i).workspace !== root.st.workspace)
                    root.showBrowser(0)
        }
    }


    // ---- menus and prompts --------------------------------------------------
    //
    // The TUI's menus (internal/ui), drawn here: Go builds them and runs the
    // item picked; QML only places them. A menu opened by a pane's right
    // click comes at the pointer.

    HoverHandler { id: pointer }

    MouseArea {
        anchors.fill: parent
        visible: !!st.menu
        z: 190
        acceptedButtons: Qt.AllButtons
        onPressed: Bridge.call("menu.close", {})
    }

    Rectangle {
        id: contextMenu
        objectName: "contextMenu"
        visible: !!st.menu
        z: 200
        readonly property var m: st.menu || ({ items: [], x: 0, y: 0, title: "" })
        width: 240
        height: menuItems.height + 34
        x: Math.min(root.width - width - 4, Math.max(4, m.x >= 0 ? m.x : pointer.point.position.x))
        y: m.y >= 0 ? Math.min(root.height - height - 4, m.y)
                    : (m.x >= 0 ? root.height - height - 30 : Math.min(root.height - height - 4, pointer.point.position.y))
        radius: 8
        color: theme.mantle
        border.color: theme.surface1
        border.width: 1
        Text {
            x: 12
            y: 8
            text: contextMenu.m.title
            color: theme.overlay0
            font.pixelSize: 11
            font.bold: true
            font.letterSpacing: 1
            font.capitalization: Font.AllUppercase
        }
        Column {
            id: menuItems
            x: 6
            y: 28
            width: parent.width - 12
            Repeater {
                model: contextMenu.m.items
                delegate: MenuItem {
                    required property string modelData
                    required property int index
                    text: modelData
                    onActivated: Bridge.call("menu.pick", { index: index })
                }
            }
        }
    }

    // A line of text an item asked for: a new name, a group, a branch.
    Rectangle {
        anchors.fill: parent
        visible: !!st.prompt
        z: 210
        color: Qt.rgba(0, 0, 0, 0.45)
        MouseArea { anchors.fill: parent; onPressed: Bridge.call("prompt.cancel", {}) }
        Rectangle {
            id: promptBox
            anchors.centerIn: parent
            width: 420
            height: 120
            radius: 10
            color: theme.mantle
            border.color: theme.surface1
            MouseArea { anchors.fill: parent }
            Text {
                x: 18
                y: 16
                text: st.prompt ? st.prompt.title : ""
                color: theme.text
                font.pixelSize: 15
                font.bold: true
            }
            Rectangle {
                x: 16
                y: 50
                width: parent.width - 32
                height: 34
                radius: 6
                color: theme.base
                border.color: theme.accent
                TextInput {
                    id: promptInput
                    anchors.fill: parent
                    anchors.leftMargin: 10
                    anchors.rightMargin: 10
                    verticalAlignment: TextInput.AlignVCenter
                    color: theme.text
                    selectionColor: theme.surface1
                    selectByMouse: true
                    font.pixelSize: 14
                    onAccepted: Bridge.call("prompt.submit", { text: text })
                    Keys.onEscapePressed: Bridge.call("prompt.cancel", {})
                }
            }
            Text {
                x: 18
                y: 92
                text: "enter to confirm · esc to cancel"
                color: theme.overlay0
                font.pixelSize: 11
            }
        }
        onVisibleChanged: {
            if (visible) {
                promptInput.text = st.prompt ? st.prompt.text : ""
                promptInput.selectAll()
                promptInput.forceActiveFocus()
            }
        }
    }


    // ---- notices ------------------------------------------------------------
    //
    // An agent that needs you, or finished, as a card at the window's foot:
    // internal/announce decided it was worth saying, as it does for the TUI.
    // A click goes to the agent.

    Column {
        anchors.right: parent.right
        anchors.bottom: statusBar.top
        anchors.margins: 12
        spacing: 8
        z: 150
        Repeater {
            model: st.toasts || []
            delegate: Rectangle {
                required property var modelData
                width: 340
                height: toastText.height + 22
                radius: 8
                color: theme.mantle
                border.width: 1
                border.color: modelData.kind === "attention" ? theme.red : theme.teal
                Rectangle {
                    x: 10
                    y: 14
                    width: 8
                    height: 8
                    radius: 4
                    color: modelData.kind === "attention" ? theme.red : theme.teal
                }
                Column {
                    id: toastText
                    x: 26
                    y: 10
                    width: parent.width - 52
                    spacing: 3
                    Text {
                        width: parent.width
                        text: modelData.title
                        color: theme.text
                        font.pixelSize: 13
                        font.bold: true
                        elide: Text.ElideRight
                    }
                    Text {
                        visible: !!modelData.body
                        width: parent.width
                        text: modelData.body || ""
                        color: theme.subtext
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                        maximumLineCount: 3
                        elide: Text.ElideRight
                    }
                }
                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: Bridge.call("toast.open", { index: modelData.id, pane: modelData.pane || 0 })
                }
                Text {
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.margins: 8
                    text: "×"
                    color: theme.overlay0
                    font.pixelSize: 14
                    MouseArea {
                        anchors.fill: parent
                        anchors.margins: -4
                        onClicked: Bridge.call("toast.dismiss", { index: modelData.id })
                    }
                }
            }
        }
    }

    Dock {
        id: dockSide
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: statusBar.top
    }

    // ---- screens ----------------------------------------------------------
    //
    // A screen the global menu opens, made only while it is open.

    Loader {
        anchors.fill: parent
        z: 180
        active: st.panel === "settings"
        source: "panels/Settings.qml"
    }
    Loader {
        anchors.fill: parent
        z: 185
        active: st.panel === "welcome"
        source: "panels/Welcome.qml"
    }

    // ---- status line -----------------------------------------------------

    Rectangle {
        id: statusBar
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        height: 24
        color: theme.crust
        Text {
            anchors.left: parent.left
            anchors.leftMargin: 10
            anchors.verticalCenter: parent.verticalCenter
            text: (st.session || "") + "  ·  " + agentModel.count + " agents"
                  + (st.waiting > 0 ? "  ·  " + st.waiting + " waiting" : "")
            color: st.waiting > 0 ? theme.red : theme.overlay0
            font.pixelSize: 12
        }
        Text {
            anchors.right: parent.right
            anchors.rightMargin: 10
            anchors.verticalCenter: parent.verticalCenter
            text: Bridge.status
            color: theme.red
            font.pixelSize: 12
        }
    }
}
