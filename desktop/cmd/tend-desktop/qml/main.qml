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
    QtObject {
        id: theme
        readonly property color base: "#1e1e2e"
        readonly property color mantle: "#181825"
        readonly property color crust: "#11111b"
        readonly property color surface0: "#313244"
        readonly property color surface1: "#45475a"
        readonly property color overlay0: "#6c7086"
        readonly property color subtext: "#a6adc8"
        readonly property color text: "#cdd6f4"
        readonly property color accent: "#89b4fa"
        readonly property color green: "#a6e3a1"
        readonly property color yellow: "#f9e2af"
        readonly property color red: "#f38ba8"
        readonly property color teal: "#94e2d5"
        readonly property color peach: "#fab387"

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

    // ---- browser tabs ------------------------------------------------------
    //
    // A browser tab is this window's, not the session's — like a folded
    // group, it is what one person is looking at — and belongs to the space
    // it was opened in. Its page is Browser.qml, made from text when the
    // first one opens, so the window has no Chromium in it until then.

    property int activeBrowser: 0
    property int nextBrowser: 1
    property var browsers: ({})

    function openBrowser(url) {
        var page
        try {
            page = Qt.createQmlObject(Bridge.browserQml, browserLayer, "Browser.qml")
        } catch (e) {
            Bridge.call("status", { text: "the browser needs Qt WebEngine: sudo apt install qml6-module-qtwebengine" })
            return
        }
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
            onClicked: entry.clicked()
        }
    }

    // ---- tabs ------------------------------------------------------------

    Rectangle {
        id: tabBar
        anchors.left: sidebar.right
        anchors.right: parent.right
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
                    color: active && root.activeBrowser === 0 ? theme.surface0 : (tabHover.containsMouse ? Qt.rgba(1, 1, 1, 0.05) : "transparent")
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
                        acceptedButtons: Qt.LeftButton | Qt.MiddleButton
                        onClicked: (mouse) => {
                            if (mouse.button === Qt.MiddleButton) Bridge.call("tab.close", { tab: tabEntry.tabId })
                            else { root.showBrowser(0); Bridge.call("tab", { tab: tabEntry.tabId }) }
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
                            else root.showBrowser(btab.bid)
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
        anchors.right: parent.right
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
            x: 38
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
