// The command palette (#71): every action, space, tab and agent by name,
// typed to and run with Enter. Opened with Ctrl+Shift+P or Ctrl+Shift+K —
// not Ctrl+K, which is the shell's (and many terminal programs') "kill to
// the end of the line", and which a window that took it would break in
// every pane.
//
// It is made from what the window already shows (st): a choice runs the
// same Bridge call a click would, so the palette cannot do anything the
// window does not.
import QtQuick
import Tend 1.0

Rectangle {
    id: palette
    color: Qt.rgba(0, 0, 0, 0.45)
    signal done()
    property int selected: 0

    function entries() {
        var out = []
        var add = function (group, label, call, args) { out.push({ group: group, label: label, call: call, args: args || {} }) }
        add("action", "New terminal tab", "tab.new")
        ;(st.newAgents || []).forEach(function (a) { add("action", "New " + a.name + " tab", "tab.agent", { agent: a.id }) })
        add("action", "New space", "workspace.new")
        add("action", "Split side by side", "pane.split", { direction: "columns" })
        add("action", "Split one over the other", "pane.split", { direction: "rows" })
        add("action", "Close pane", "pane.close")
        add("action", "Zoom pane", "pane.zoom", { pane: st.focus || 0 })
        add("action", "Settings", "panel", { name: "settings" })
        add("action", "Files", "dock", { name: "files" })
        add("action", "Tasks", "dock", { name: "tasks" })
        add("action", "Reload settings", "settings.reload")
        ;(st.spaces || []).forEach(function (r) { if (r.kind === "space") add("space", r.label, "workspace", { workspace: r.workspace }) })
        ;(st.tabs || []).forEach(function (t) { add("tab", t.name, "tab", { tab: t.id }) })
        ;(st.agents || []).forEach(function (r) {
            if (r.kind === "agent") {
                var words = (r.lines || []).map(function (l) { return l.map(function (t) { return t.text }).join(" ") }).join(" · ")
                add("agent", words || r.label, "jump", { pane: r.pane })
            }
        })
        return out
    }

    // A subsequence match, scored: letters close together and at word
    // starts first, as editors' palettes rank.
    function score(label, query) {
        if (query === "") return 1
        var l = label.toLowerCase(), q = query.toLowerCase(), at = -1, s = 0, last = -2
        for (var i = 0; i < q.length; i++) {
            at = l.indexOf(q[i], at + 1)
            if (at < 0) return 0
            s += (at === last + 1 ? 3 : 1) + (at === 0 || l[at - 1] === " " ? 2 : 0)
            last = at
        }
        return s
    }
    property var shown: {
        var q = input.text
        // Ties keep the list's own order: actions, spaces, tabs, agents.
        return entries().map(function (e, i) { return { e: e, s: score(e.label, q), i: i } })
                        .filter(function (x) { return x.s > 0 })
                        .sort(function (a, b) { return b.s - a.s || a.i - b.i })
                        .slice(0, 40).map(function (x) { return x.e })
    }
    function run(e) {
        if (!e) return
        palette.done()
        Bridge.call(e.call, e.args)
    }

    MouseArea { anchors.fill: parent; onPressed: palette.done() }

    Rectangle {
        anchors.horizontalCenter: parent.horizontalCenter
        y: 80
        width: Math.min(parent.width - 80, 620)
        height: 52 + Math.min(list.contentHeight, 420)
        radius: 10
        color: theme.mantle
        border.color: theme.surface1
        MouseArea { anchors.fill: parent }

        TextInput {
            id: input
            x: 16
            y: 14
            width: parent.width - 32
            color: theme.text
            font.pixelSize: 16
            focus: true
            Component.onCompleted: forceActiveFocus()
            onTextChanged: palette.selected = 0
            Keys.onEscapePressed: palette.done()
            Keys.onDownPressed: palette.selected = Math.min(palette.shown.length - 1, palette.selected + 1)
            Keys.onUpPressed: palette.selected = Math.max(0, palette.selected - 1)
            onAccepted: palette.run(palette.shown[palette.selected])
            Text {
                visible: input.text === ""
                text: "type an action, a space, a tab or an agent"
                color: theme.overlay0
                font.pixelSize: 16
            }
        }
        ListView {
            id: list
            x: 8
            y: 46
            width: parent.width - 16
            height: Math.min(contentHeight, 420)
            clip: true
            model: palette.shown
            currentIndex: palette.selected
            delegate: Rectangle {
                required property var modelData
                required property int index
                width: list.width
                height: 32
                radius: 6
                color: index === palette.selected ? theme.surface0 : "transparent"
                Text {
                    x: 10
                    anchors.verticalCenter: parent.verticalCenter
                    width: 54
                    text: modelData.group
                    color: theme.overlay0
                    font.pixelSize: 11
                }
                Text {
                    x: 70
                    anchors.verticalCenter: parent.verticalCenter
                    width: parent.width - 80
                    text: modelData.label
                    color: theme.text
                    font.pixelSize: 14
                    elide: Text.ElideRight
                }
                MouseArea {
                    anchors.fill: parent
                    hoverEnabled: true
                    onEntered: palette.selected = index
                    onClicked: palette.run(modelData)
                }
            }
        }
    }
}
