// The dock's tasks: the session's plan of the work (issue #72), grouped by
// the day each is for, with the form for a new one. A task given an agent
// and a time starts that agent then, in its space, with its first message:
// the server does it, so it happens with the window closed.
import QtQuick
import Tend 1.0

Item {
    id: tasks
    readonly property var list: (st.dock && st.dock.tasks) || []
    property bool adding: false

    Column {
        id: head
        width: parent.width
        spacing: 8
        Row {
            width: parent.width
            Text {
                width: parent.width - newButton.width
                text: "Tasks"
                color: theme.text
                font.pixelSize: 16
                font.bold: true
            }
            Rectangle {
                id: newButton
                width: 72
                height: 26
                radius: 6
                color: newArea.containsMouse ? theme.surface1 : theme.surface0
                Text { anchors.centerIn: parent; text: tasks.adding ? "cancel" : "+ new"; color: theme.text; font.pixelSize: 12 }
                MouseArea { id: newArea; anchors.fill: parent; hoverEnabled: true; onClicked: tasks.adding = !tasks.adding }
            }
        }

        // The new task. Times are as the CLI reads them: 09:00 (the next
        // one), +30m, 2026-11-27 09:00.
        Column {
            visible: tasks.adding
            width: parent.width
            spacing: 6
            Field { id: fTitle; width: parent.width; hint: "what is to be done" }
            Row {
                width: parent.width
                spacing: 6
                Field { id: fAt; width: (parent.width - 6) / 2; hint: "start at (09:00, +30m)" }
                Field { id: fDue; width: (parent.width - 6) / 2; hint: "due (2026-11-28)" }
            }
            Row {
                spacing: 4
                Text { text: "agent:"; color: theme.subtext; font.pixelSize: 12; anchors.verticalCenter: parent.verticalCenter }
                Repeater {
                    model: [{ id: "", name: "none" }].concat(st.newAgents || [])
                    delegate: Rectangle {
                        required property var modelData
                        readonly property bool chosen: tasks.agent === modelData.id
                        height: 24
                        width: agentName.width + 14
                        radius: 5
                        color: chosen ? theme.accent : theme.surface0
                        Text { id: agentName; anchors.centerIn: parent; text: modelData.name; font.pixelSize: 11; color: chosen ? theme.base : theme.text }
                        MouseArea { anchors.fill: parent; onClicked: tasks.agent = modelData.id }
                    }
                }
            }
            Field { id: fPrompt; width: parent.width; visible: tasks.agent !== ""; hint: "its first message" }
            Row {
                spacing: 8
                Rectangle {
                    width: 80
                    height: 28
                    radius: 6
                    color: theme.accent
                    Text { anchors.centerIn: parent; text: "add"; color: theme.base; font.bold: true; font.pixelSize: 13 }
                    MouseArea {
                        anchors.fill: parent
                        onClicked: {
                            Bridge.call("task.create", { title: fTitle.text, at: fAt.text, due: fDue.text,
                                                         agent: tasks.agent, prompt: fPrompt.text, here: true })
                            fTitle.text = ""; fAt.text = ""; fDue.text = ""; fPrompt.text = ""
                            tasks.agent = ""
                            tasks.adding = false
                        }
                    }
                }
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "in this space"
                    color: theme.overlay0
                    font.pixelSize: 11
                }
            }
        }
    }
    property string agent: ""

    ListView {
        anchors.top: head.bottom
        anchors.topMargin: 10
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        spacing: 4
        model: tasks.list
        section.property: "day"
        section.delegate: Text {
            required property string section
            topPadding: 8
            bottomPadding: 2
            text: section === "" ? "ANY DAY" : new Date(section + "T00:00:00").toDateString().toUpperCase()
            color: theme.overlay0
            font.pixelSize: 11
            font.bold: true
            font.letterSpacing: 1
        }
        delegate: Rectangle {
            id: task
            required property var modelData
            width: ListView.view.width
            height: col.height + 14
            radius: 6
            color: taskHover.containsMouse ? theme.surface0 : "transparent"
            MouseArea { id: taskHover; anchors.fill: parent; hoverEnabled: true; acceptedButtons: Qt.NoButton }
            // Done or not, at a click.
            Rectangle {
                x: 6
                y: 9
                width: 16
                height: 16
                radius: 4
                color: task.modelData.status === "done" ? theme.green : "transparent"
                border.color: task.modelData.status === "doing" ? theme.yellow : theme.overlay0
                border.width: 1.5
                Text { anchors.centerIn: parent; visible: task.modelData.status === "done"; text: "✓"; color: theme.base; font.pixelSize: 11 }
                MouseArea {
                    anchors.fill: parent
                    anchors.margins: -4
                    onClicked: Bridge.call("task.status", { id: task.modelData.id, status: task.modelData.status === "done" ? "todo" : "done" })
                }
            }
            Column {
                id: col
                x: 30
                y: 7
                width: parent.width - 60
                spacing: 2
                Text {
                    width: parent.width
                    text: task.modelData.title
                    color: task.modelData.status === "done" ? theme.overlay0 : theme.text
                    font.strikeout: task.modelData.status === "done"
                    font.pixelSize: 13
                    wrapMode: Text.Wrap
                }
                Text {
                    width: parent.width
                    visible: text !== ""
                    text: [task.modelData.startAt ? "starts " + task.modelData.startAt : "",
                           task.modelData.due ? "due " + task.modelData.due : "",
                           task.modelData.agent ? "✦ " + task.modelData.agent : "",
                           task.modelData.workspace ? "in " + task.modelData.workspace : ""].filter(Boolean).join(" · ")
                    color: theme.subtext
                    font.pixelSize: 11
                    wrapMode: Text.Wrap
                }
                Text {
                    visible: !!task.modelData.error
                    width: parent.width
                    text: task.modelData.error || ""
                    color: theme.red
                    font.pixelSize: 11
                    wrapMode: Text.Wrap
                }
                Text {
                    visible: task.modelData.pane > 0
                    text: "go to its agent →"
                    color: theme.accent
                    font.pixelSize: 11
                    MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: Bridge.call("jump", { pane: task.modelData.pane }) }
                }
            }
            Text {
                anchors.right: parent.right
                anchors.rightMargin: 8
                y: 8
                visible: taskHover.containsMouse
                text: "×"
                color: theme.overlay0
                font.pixelSize: 14
                MouseArea { anchors.fill: parent; anchors.margins: -4; onClicked: Bridge.call("task.delete", { id: task.modelData.id }) }
            }
        }
        // The trackers' tickets, beside the tasks: the plan of the work in
        // one view. A click opens one in the tickets tab.
        footer: Column {
            width: ListView.view ? ListView.view.width : 0
            readonly property var tk: st.dock && st.dock.tickets
            readonly property var open: tk ? (tk.tickets || []).filter(function (t) { return !t.done }) : []
            visible: !!tk
            spacing: 4
            topPadding: 14
            Text {
                text: tk ? "TICKETS · " + tk.account : ""
                color: theme.overlay0
                font.pixelSize: 11
                font.bold: true
                font.letterSpacing: 1
            }
            Repeater {
                model: parent.open
                delegate: Rectangle {
                    required property var modelData
                    width: parent ? parent.width : 0
                    height: 38
                    radius: 6
                    color: tkArea.containsMouse ? theme.surface0 : "transparent"
                    Text { x: 8; y: 4; text: modelData.key + " · " + modelData.state; color: theme.overlay0; font.pixelSize: 11 }
                    Text { x: 8; y: 19; width: parent.width - 16; text: modelData.title; color: theme.text; font.pixelSize: 12; elide: Text.ElideRight }
                    MouseArea {
                        id: tkArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: {
                            Bridge.call("dock", { name: "tickets" })
                            Bridge.call("tickets", { do: "open", key: modelData.key })
                        }
                    }
                }
            }
        }
        Text {
            visible: tasks.list.length === 0 && !(st.dock && st.dock.loading)
            text: "No tasks yet. + new plans one, with an agent to start at a time if you like."
            color: theme.overlay0
            font.pixelSize: 12
            width: parent.width
            wrapMode: Text.Wrap
        }
    }

    component Field: Rectangle {
        property alias text: input.text
        property string hint: ""
        height: 30
        radius: 6
        color: theme.base
        border.color: input.activeFocus ? theme.accent : theme.surface1
        TextInput {
            id: input
            anchors.fill: parent
            anchors.leftMargin: 8
            anchors.rightMargin: 8
            verticalAlignment: TextInput.AlignVCenter
            color: theme.text
            font.pixelSize: 13
            clip: true
            selectByMouse: true
        }
        Text {
            visible: input.text === "" && !input.activeFocus
            anchors.left: parent.left
            anchors.leftMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            text: parent.hint
            color: theme.overlay0
            font.pixelSize: 12
        }
    }
}
