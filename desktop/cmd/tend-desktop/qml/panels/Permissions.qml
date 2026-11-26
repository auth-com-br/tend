// The agents' permissions: what each agent may do without asking, a tab an
// agent, for the project of the pane in focus when it opened — the TUI's
// permissions manager (internal/tools). A rule's decision is clicked to
// change it; its file is picked from the agent's own to move it; a mode's
// value is picked from its choices. The rules are read and written by the
// session's server.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: { const p = st.perm || {}; return Object.assign({}, p, { tabs: p.tabs || [], lines: p.lines || [], decisions: p.decisions || [], files: p.files || [], ruleFiles: p.ruleFiles || [] }) }
    // What a pick menu is for: "", "decision", "move", "mode", "open", "editor".
    property string picking: ""
    property int pickRow: -1
    // A rule being added: its decision and file.
    property string addDecision: "allow"
    property int addFile: -1
    title: "Agent permissions"
    subtitle: s.where || ""
    message: s.message || ""
    alert: !!s.alert
    loading: !!s.loading
    confirm: s.confirm || ""
    detail: s.detail || []
    boxWidth: 980
    onAnswered: (yes) => Bridge.call("perm", { do: "answer", yes: yes })
    onClosed: Bridge.call("panel", { name: "" })
    onDetailClosed: Bridge.call("perm", { do: "reload" })

    function act(args) { Bridge.call("perm", args) }
    function pick(what, row) { screen.picking = what; screen.pickRow = row }

    // The agents.
    Row {
        id: tabs
        width: parent.width
        spacing: 4
        Repeater {
            model: screen.s.tabs
            delegate: Rectangle {
                required property var modelData
                required property int index
                readonly property bool chosen: index === screen.s.tab
                height: 28
                width: tabText.width + 22
                radius: 6
                color: chosen ? theme.accent : (tabArea.containsMouse ? theme.surface1 : theme.surface0)
                Text {
                    id: tabText
                    anchors.centerIn: parent
                    text: modelData.Name + " " + modelData.Rules + (modelData.Warn ? " !" : "")
                    color: chosen ? theme.base : (modelData.Warn ? theme.yellow : theme.text)
                    font.pixelSize: 12
                }
                MouseArea { id: tabArea; anchors.fill: parent; hoverEnabled: true; onClicked: screen.act({ do: "tab", index: index }) }
            }
        }
    }

    Column {
        id: notes
        y: tabs.height + 8
        width: parent.width
        Repeater {
            model: screen.s.notes || []
            delegate: Text {
                required property string modelData
                width: notes.width
                text: modelData
                color: modelData.startsWith("!") ? theme.yellow : theme.overlay0
                font.pixelSize: 12
                wrapMode: Text.Wrap
            }
        }
    }

    Row {
        id: tools
        y: notes.y + notes.height + 8
        width: parent.width
        spacing: 6
        ToolField {
            id: filter
            width: 240
            hint: "show rules with…"
            onEdited: screen.act({ do: "filter", text: text })
        }
        ToolButton { label: "open a file…"; onClicked: screen.pick("open", -1) }
        ToolButton { label: "open in…"; onClicked: { screen.act({ do: "editors" }); screen.pick("editor", -1) } }
        ToolButton { label: "reload"; onClicked: screen.act({ do: "reload" }) }
        ToolButton { label: "what is this?"; onClicked: screen.act({ do: "help" }) }
    }

    ListView {
        id: list
        y: tools.y + tools.height + 8
        width: parent.width
        height: parent.height - y - addRow.height - 12
        clip: true
        spacing: 2
        model: screen.s.lines
        boundsBehavior: Flickable.StopAtBounds
        delegate: Rectangle {
            id: line
            required property var modelData
            required property int index
            width: list.width
            height: 32
            radius: 6
            color: lineHover.containsMouse ? theme.surface0 : "transparent"
            MouseArea { id: lineHover; anchors.fill: parent; hoverEnabled: true; acceptedButtons: Qt.NoButton }
            Row {
                x: 6
                anchors.verticalCenter: parent.verticalCenter
                spacing: 8
                // The decision, or "mode": a click changes it.
                ToolButton {
                    width: 62
                    label: line.modelData.Mode ? "mode" : line.modelData.Decision
                    active: !line.modelData.ReadOnly
                    onClicked: line.modelData.Mode ? screen.pick("mode", line.index) : screen.pick("decision", line.index)
                }
                Text {
                    width: 70
                    anchors.verticalCenter: parent.verticalCenter
                    text: line.modelData.Scope
                    color: theme.subtext
                    font.pixelSize: 12
                }
                Text {
                    width: 22
                    anchors.verticalCenter: parent.verticalCenter
                    text: line.modelData.Why ? "⚠" : (line.modelData.Elsewhere ? "≡" : "")
                    color: line.modelData.Why ? theme.yellow : theme.overlay0
                    font.pixelSize: 13
                }
                Text {
                    width: line.width - 62 - 70 - 22 - lineButtons.width - 50
                    anchors.verticalCenter: parent.verticalCenter
                    text: line.modelData.Text
                    color: line.modelData.ReadOnly ? theme.overlay0 : theme.text
                    font.family: "monospace"
                    font.pixelSize: 12
                    elide: Text.ElideRight
                }
            }
            Row {
                id: lineButtons
                anchors.right: parent.right
                anchors.rightMargin: 6
                anchors.verticalCenter: parent.verticalCenter
                spacing: 4
                visible: !line.modelData.Mode
                ToolButton { label: "details"; onClicked: screen.act({ do: "detail", row: line.index }) }
                ToolButton { label: "move…"; active: !line.modelData.ReadOnly; onClicked: screen.pick("move", line.index) }
                ToolButton { label: "edit"; onClicked: screen.act({ do: "open", row: line.index, index: -1 }) }
                ToolButton { label: "remove"; danger: true; active: !line.modelData.ReadOnly; onClicked: screen.act({ do: "remove", row: line.index }) }
            }
        }
        Text {
            visible: !screen.s.loading && screen.s.lines.length === 0
            y: 10
            x: 8
            width: list.width - 16
            text: screen.s.empty || ""
            color: theme.overlay0
            font.pixelSize: 13
            wrapMode: Text.Wrap
        }
    }

    // A new rule: its text, what it says, and the file it goes in.
    Column {
        id: addRow
        anchors.bottom: parent.bottom
        width: parent.width
        spacing: 4
        Text {
            width: parent.width
            text: screen.s.hint || ""
            color: theme.overlay0
            font.pixelSize: 11
            elide: Text.ElideRight
        }
        Row {
            width: parent.width
            spacing: 6
            ToolField {
                id: ruleLine
                width: parent.width - decisionButton.width - fileButton.width - addButton.width - 18
                hint: "add a rule"
                onAccepted: addButton.clicked()
            }
            ToolButton {
                id: decisionButton
                anchors.verticalCenter: parent.verticalCenter
                label: screen.addDecision + " ▾"
                onClicked: {
                    const ds = screen.s.decisions
                    let i = 0
                    for (let k = 0; k < ds.length; k++) if (ds[k].value === screen.addDecision) i = k
                    if (ds.length > 0) screen.addDecision = ds[(i + 1) % ds.length].value
                }
            }
            ToolButton {
                id: fileButton
                anchors.verticalCenter: parent.verticalCenter
                label: (screen.addFile >= 0 && screen.addFile < screen.s.files.length
                        ? screen.s.files[screen.addFile].split("  ")[0] : "file") + " ▾"
                onClicked: screen.pick("addfile", -1)
            }
            ToolButton {
                id: addButton
                anchors.verticalCenter: parent.verticalCenter
                label: "add"
                primary: true
                onClicked: {
                    if (ruleLine.text.trim() === "") return
                    if (screen.addFile < 0) { screen.pick("addfile", -1); return }
                    screen.act({ do: "add", text: ruleLine.text.trim(), decision: screen.addDecision, index: screen.addFile })
                    ruleLine.text = ""
                }
            }
        }
    }

    // What a click above asked to pick from: a decision, a file, a mode's
    // value, an editor.
    Rectangle {
        visible: screen.picking !== ""
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.35)
        MouseArea { anchors.fill: parent; onClicked: screen.picking = "" }
        Rectangle {
            anchors.centerIn: parent
            width: Math.min(parent.width - 40, 620)
            height: Math.min(parent.height - 40, pickList.contentHeight + 56)
            radius: 8
            color: theme.mantle
            border.color: theme.surface1
            MouseArea { anchors.fill: parent }
            Text {
                x: 14
                y: 10
                text: ({ decision: "the rule says", move: "move it to", mode: "set it to", open: "open which file?",
                         editor: "open its file in", addfile: "into which file?" })[screen.picking] || ""
                color: theme.text
                font.bold: true
                font.pixelSize: 14
            }
            ListView {
                id: pickList
                x: 8
                y: 36
                width: parent.width - 16
                height: parent.height - 44
                clip: true
                model: {
                    const s = screen.s
                    const line = screen.pickRow >= 0 ? s.lines[screen.pickRow] : null
                    switch (screen.picking) {
                    case "decision": return s.decisions.map(d => ({ label: d.label, value: d.value }))
                    case "mode": return (line && line.choices || []).map(c => ({ label: c + (c === line.value ? "  ✓" : ""), value: c }))
                                        .concat([{ label: "not set: the agent's default", value: "" }])
                    case "move": return s.ruleFiles.filter(i => !line || i !== line.file).map(i => ({ label: s.files[i], value: i }))
                    case "addfile": return s.ruleFiles.map(i => ({ label: s.files[i], value: i }))
                    case "open": return s.files.map((f, i) => ({ label: f, value: i }))
                    case "editor": return (s.editors || []).map((e, i) => ({ label: e, value: i }))
                    }
                    return []
                }
                delegate: Rectangle {
                    required property var modelData
                    width: pickList.width
                    height: 30
                    radius: 5
                    color: pickArea.containsMouse ? theme.surface0 : "transparent"
                    Text {
                        x: 8
                        anchors.verticalCenter: parent.verticalCenter
                        width: parent.width - 16
                        text: modelData.label
                        color: theme.text
                        font.pixelSize: 12
                        elide: Text.ElideRight
                    }
                    MouseArea {
                        id: pickArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: {
                            const v = modelData.value
                            switch (screen.picking) {
                            case "decision": screen.act({ do: "decision", row: screen.pickRow, text: v }); break
                            case "mode": screen.act({ do: "mode", row: screen.pickRow, text: v }); break
                            case "move": screen.act({ do: "move", row: screen.pickRow, index: v }); break
                            case "addfile": screen.addFile = v; break
                            case "open": screen.act({ do: "open", row: -1, index: v }); break
                            case "editor": screen.act({ do: "open", row: -1, index: -1, editorAt: v }); break
                            }
                            screen.picking = ""
                        }
                    }
                }
            }
        }
    }
}
