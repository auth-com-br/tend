// The dock's WhatsApp (issue #73): tend-whatsapp's daemon, read over its
// socket — whether the number is linked, the chats, a chat's messages, a
// search, and the replies the agents proposed. A reply is sent only from
// the confirmation, which shows its exact text and chat: the agents
// propose, the owner sends. Everything a customer wrote is shown as plain
// text, never as markup.
import QtQuick
import Tend 1.0

Item {
    id: wa
    readonly property var s: (st.dock && st.dock.whatsapp) || null
    readonly property bool linked: !!(s && s.running && s.linked)

    function act(args) { Bridge.call("whatsapp", args) }

    Column {
        id: head
        width: parent.width
        spacing: 8
        Row {
            width: parent.width
            spacing: 8
            Rectangle {
                width: 9
                height: 9
                radius: 5
                anchors.verticalCenter: parent.verticalCenter
                color: !wa.s || !wa.s.running ? theme.overlay0
                     : !wa.s.linked ? theme.overlay0
                     : wa.s.connected ? theme.green : theme.yellow
            }
            Text {
                width: parent.width - 17 - refresh.width
                text: "WhatsApp"
                color: theme.text
                font.pixelSize: 16
                font.bold: true
            }
            Text {
                id: refresh
                text: "↻"
                color: refreshArea.containsMouse ? theme.accent : theme.overlay0
                font.pixelSize: 15
                MouseArea { id: refreshArea; anchors.fill: parent; anchors.margins: -4; hoverEnabled: true; onClicked: wa.act({ do: "refresh" }) }
            }
        }
        Text {
            width: parent.width
            wrapMode: Text.Wrap
            font.pixelSize: 12
            color: theme.subtext
            textFormat: Text.PlainText
            text: !wa.s ? "" :
                  !wa.s.running ? "tend-whatsapp's daemon is not running." :
                  !wa.s.linked ? "No number is linked yet." :
                  (wa.s.name ? wa.s.name + " · " : "") + (wa.s.number || "") +
                  (wa.s.connected ? " · connected" : " · not connected now") +
                  (wa.s.kept ? "\n" + wa.s.kept : "")
        }

        // Not running, or not linked: what to do about it.
        Column {
            visible: !!wa.s && !wa.linked
            width: parent.width
            spacing: 8
            Text {
                width: parent.width
                wrapMode: Text.Wrap
                font.pixelSize: 12
                color: theme.overlay0
                text: "Linking adds this machine to your number as a companion device, as WhatsApp Web does: "
                      + "a QR code to scan with the phone, shown in a terminal. It is against WhatsApp's terms, "
                      + "and the number can be banned for it. The conversations stay on this machine; the agents "
                      + "read them and propose replies, and only you send."
            }
            Row {
                spacing: 8
                Button { label: "link in a new tab"; primary: true; onClicked: wa.act({ do: "link" }) }
                Button {
                    visible: !!wa.s && !wa.s.running
                    label: wa.s && wa.s.busy ? "starting…" : "start the daemon"
                    onClicked: wa.act({ do: "start" })
                }
            }
        }

        // The replies waiting for the owner, first: an agent is waiting on
        // each one.
        Text {
            visible: wa.linked && wa.s.replies.length > 0
            topPadding: 4
            text: "WAITING FOR YOU · " + (wa.s ? wa.s.replies.length : 0)
            color: theme.yellow
            font.pixelSize: 11
            font.bold: true
            font.letterSpacing: 1
        }
        Repeater {
            model: wa.linked ? wa.s.replies : []
            delegate: Rectangle {
                id: reply
                required property var modelData
                width: head.width
                height: replyCol.height + 16
                radius: 7
                color: theme.surface0
                border.color: modelData.state === "failed" ? theme.red : theme.surface1
                Column {
                    id: replyCol
                    x: 10
                    y: 8
                    width: parent.width - 20
                    spacing: 4
                    Text {
                        width: parent.width
                        text: "to " + reply.modelData.chatName + (reply.modelData.by ? "  ·  from " + reply.modelData.by : "") + "  ·  " + reply.modelData.at
                        textFormat: Text.PlainText
                        color: theme.subtext
                        font.pixelSize: 11
                        elide: Text.ElideRight
                    }
                    Text {
                        width: parent.width
                        text: reply.modelData.text
                        textFormat: Text.PlainText
                        color: theme.text
                        font.pixelSize: 13
                        wrapMode: Text.Wrap
                        maximumLineCount: 4
                        elide: Text.ElideRight
                    }
                    Text {
                        visible: !!reply.modelData.error
                        width: parent.width
                        text: "the send failed: " + (reply.modelData.error || "")
                        textFormat: Text.PlainText
                        color: theme.red
                        font.pixelSize: 11
                        wrapMode: Text.Wrap
                    }
                    Row {
                        spacing: 6
                        Button { label: "approve…"; primary: true; onClicked: wa.act({ do: "confirm", id: reply.modelData.id }) }
                        Button { label: "reject"; onClicked: wa.act({ do: "reject", id: reply.modelData.id }) }
                    }
                }
            }
        }

        // Search, over every chat kept.
        Rectangle {
            visible: wa.linked
            width: parent.width
            height: 30
            radius: 6
            color: theme.base
            border.color: query.activeFocus ? theme.accent : theme.surface1
            TextInput {
                id: query
                anchors.fill: parent
                anchors.leftMargin: 8
                anchors.rightMargin: 8
                verticalAlignment: TextInput.AlignVCenter
                color: theme.text
                font.pixelSize: 13
                clip: true
                selectByMouse: true
                onAccepted: wa.act({ do: "search", query: text })
            }
            Text {
                visible: query.text === "" && !query.activeFocus
                anchors.left: parent.left
                anchors.leftMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                text: "search the messages (enter)"
                color: theme.overlay0
                font.pixelSize: 12
            }
        }
        Row {
            visible: wa.linked && (!!wa.s.open || !!wa.s.query)
            spacing: 8
            Text {
                text: "← " + (wa.s && wa.s.open ? wa.s.open.name : "")
                visible: !!(wa.s && wa.s.open)
                textFormat: Text.PlainText
                color: theme.accent
                font.pixelSize: 13
                font.bold: true
                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: wa.act({ do: "back" }) }
            }
            Text {
                visible: !!(wa.s && !!wa.s.query && !wa.s.open)
                text: "found for “" + (wa.s ? wa.s.query : "") + "” · clear"
                textFormat: Text.PlainText
                color: theme.accent
                font.pixelSize: 12
                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: { query.text = ""; wa.act({ do: "search", query: "" }) } }
            }
        }
    }

    // A chat's messages, the results of a search, or the chats.
    ListView {
        id: list
        visible: wa.linked
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 24
        width: parent.width
        clip: true
        spacing: 4
        readonly property string mode: !wa.s ? "" : wa.s.open ? "chat" : !!wa.s.query ? "found" : "chats"
        model: !wa.s ? [] : mode === "chat" ? (wa.s.messages || []) : mode === "found" ? (wa.s.results || []) : wa.s.chats
        onCountChanged: if (mode === "chat") positionViewAtEnd()
        delegate: Loader {
            required property var modelData
            width: ListView.view.width
            sourceComponent: list.mode === "chats" ? chatRow : message
            property var row: modelData
        }
        Text {
            visible: list.count === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: list.mode === "found" ? "Nothing found." : "No chats kept yet: they arrive as WhatsApp sends them."
            color: theme.overlay0
            font.pixelSize: 12
        }
    }

    Component {
        id: chatRow
        Rectangle {
            readonly property var c: parent.row
            height: 46
            radius: 6
            color: chatArea.containsMouse ? theme.surface0 : "transparent"
            Text {
                x: 8
                y: 6
                width: parent.width - 70
                text: c.name
                textFormat: Text.PlainText
                color: theme.text
                font.pixelSize: 13
                elide: Text.ElideRight
            }
            Text {
                anchors.right: parent.right
                anchors.rightMargin: 8
                y: 7
                text: c.last || ""
                color: theme.overlay0
                font.pixelSize: 11
            }
            Text {
                x: 8
                y: 25
                width: parent.width - 16
                text: c.lastText || ""
                textFormat: Text.PlainText
                color: theme.subtext
                font.pixelSize: 11
                elide: Text.ElideRight
            }
            MouseArea { id: chatArea; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: wa.act({ do: "chat", chat: c.jid }) }
        }
    }

    Component {
        id: message
        Item {
            readonly property var m: parent.row
            height: bubble.height + 2
            Rectangle {
                id: bubble
                x: m.fromMe ? parent.width - width : 0
                width: Math.min(parent.width * 0.85, Math.max(body.implicitWidth, meta.implicitWidth) + 18)
                height: body.height + meta.height + 12
                radius: 8
                color: m.fromMe ? Qt.darker(theme.accent, 2.2) : theme.surface0
                Text {
                    id: meta
                    x: 9
                    y: 5
                    width: parent.width - 18
                    text: m.from + " · " + m.at
                    textFormat: Text.PlainText
                    color: m.fromMe ? theme.text : theme.subtext
                    opacity: m.fromMe ? 0.75 : 1
                    font.pixelSize: 10
                    elide: Text.ElideRight
                }
                Text {
                    id: body
                    x: 9
                    anchors.top: meta.bottom
                    anchors.topMargin: 2
                    width: Math.min(implicitWidth, bubble.parent.width * 0.85 - 18)
                    text: m.text
                    textFormat: Text.PlainText
                    color: theme.text
                    font.pixelSize: 13
                    font.italic: m.kind !== "text"
                    wrapMode: Text.Wrap
                }
                MouseArea {
                    anchors.fill: parent
                    enabled: list.mode === "found"
                    cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
                    onClicked: wa.act({ do: "chat", chat: m.chat })
                }
            }
        }
    }

    // The confirmation: what will be sent, to whom, exactly. "send" is the
    // only thing in the window that calls the daemon's approve.
    Rectangle {
        visible: !!(wa.s && wa.s.confirm)
        anchors.fill: parent
        anchors.margins: -14
        color: Qt.rgba(0, 0, 0, 0.55)
        MouseArea { anchors.fill: parent }
        Rectangle {
            anchors.centerIn: parent
            width: parent.width - 28
            height: confirmCol.height + 28
            radius: 10
            color: theme.mantle
            border.color: theme.accent
            Column {
                id: confirmCol
                x: 14
                y: 14
                width: parent.width - 28
                spacing: 10
                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "Send this to " + (wa.s && wa.s.confirm ? wa.s.confirm.chatName : "") + "?"
                    textFormat: Text.PlainText
                    color: theme.text
                    font.pixelSize: 15
                    font.bold: true
                }
                Text {
                    width: parent.width
                    text: wa.s && wa.s.confirm ? wa.s.confirm.chat : ""
                    textFormat: Text.PlainText
                    color: theme.overlay0
                    font.pixelSize: 11
                }
                Rectangle {
                    width: parent.width
                    height: exact.height + 16
                    radius: 6
                    color: theme.base
                    border.color: theme.surface1
                    Text {
                        id: exact
                        x: 8
                        y: 8
                        width: parent.width - 16
                        text: wa.s && wa.s.confirm ? wa.s.confirm.text : ""
                        textFormat: Text.PlainText
                        color: theme.text
                        font.pixelSize: 13
                        wrapMode: Text.Wrap
                    }
                }
                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "It goes from your number, in your name"
                          + (wa.s && wa.s.confirm && wa.s.confirm.by ? ", proposed by " + wa.s.confirm.by : "") + "."
                    textFormat: Text.PlainText
                    color: theme.subtext
                    font.pixelSize: 11
                }
                Row {
                    spacing: 8
                    Button {
                        label: "send"
                        primary: true
                        onClicked: wa.act({ do: "approve", id: wa.s.confirm.id })
                    }
                    Button { label: "cancel"; onClicked: wa.act({ do: "cancel" }) }
                }
            }
        }
    }

    component Button: Rectangle {
        id: button
        property string label: ""
        property bool primary: false
        signal clicked()
        width: buttonText.implicitWidth + 20
        height: 26
        radius: 6
        color: primary ? (buttonArea.containsMouse ? Qt.lighter(theme.accent, 1.1) : theme.accent)
                       : (buttonArea.containsMouse ? theme.overlay0 : theme.surface1)
        Text {
            id: buttonText
            anchors.centerIn: parent
            text: button.label
            color: button.primary ? theme.base : theme.text
            font.pixelSize: 12
            font.bold: button.primary
        }
        MouseArea { id: buttonArea; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: button.clicked() }
    }
}
