// The window's sidebar: the session's spaces and its agents, drawn as a
// window draws them — cards with a name, where it is and what it is doing —
// not as the TUI's sidebar of tokens laid in a terminal's columns. The rows
// and their order are still internal/sidebar's, the TUI's own (spaceModel,
// agentModel); what each says comes from its card (core.SpaceCard,
// core.AgentCard). Its width is the person's, kept in the window only.
import QtQuick
import Tend 1.0

Rectangle {
    id: bar
    color: theme.mantle
    property real wanted: 280

    // ---- header: the session, a new space, the menu --------------------
    Item {
        id: header
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: 48
        Rectangle {
            id: mark
            x: 14
            anchors.verticalCenter: parent.verticalCenter
            width: 22
            height: 22
            radius: 6
            color: theme.accent
            Text {
                anchors.centerIn: parent
                text: "t"
                color: theme.base
                font.pixelSize: 14
                font.bold: true
            }
        }
        Text {
            anchors.left: mark.right
            anchors.leftMargin: 10
            anchors.right: headerButtons.left
            anchors.rightMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            text: st.session || "tend"
            color: theme.text
            font.pixelSize: 14
            font.weight: Font.DemiBold
            elide: Text.ElideRight
        }
        Row {
            id: headerButtons
            anchors.right: parent.right
            anchors.rightMargin: 10
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2
            IconButton {
                glyph: "+"
                tip: "New space"
                onClicked: Bridge.call("workspace.new", {})
            }
            IconButton {
                id: menuButton
                glyph: "⋯"
                tip: "Menu"
                dot: st.update === true
                onClicked: {
                    var p = mapToItem(root.contentItem, 0, height + 4)
                    Bridge.call("menu.open", { kind: "global", x: Math.round(p.x), y: Math.round(p.y) })
                }
            }
        }
    }

    Flickable {
        id: scroller
        anchors.top: header.bottom
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        contentHeight: body.height + 16
        clip: true
        boundsBehavior: Flickable.StopAtBounds

        Column {
            id: body
            width: scroller.width
            spacing: 2

            SectionTitle {
                text: "Spaces"
                count: spaceCount()
            }
            Repeater {
                model: spaceModel
                delegate: Loader {
                    required property var model
                    width: body.width
                    sourceComponent: model.kind === "group" ? groupRow : spaceCard
                    property var row: model
                }
            }

            Item { width: 1; height: 14 }

            SectionTitle {
                text: "Agents"
                count: agentCount()
                Segmented {
                    anchors.right: parent.right
                    anchors.rightMargin: 12
                    anchors.verticalCenter: parent.verticalCenter
                    leftText: "All"
                    rightText: "By tab"
                    rightOn: st.grouped === true
                    onPicked: (r) => { if (r !== (st.grouped === true)) Bridge.call("agents.grouped", {}) }
                }
            }
            Repeater {
                model: agentModel
                delegate: Loader {
                    required property var model
                    width: body.width
                    sourceComponent: model.kind === "tab" ? tabHeading : agentCard
                    property var row: model
                }
            }
            Text {
                visible: agentModel.count === 0
                x: 16
                width: body.width - 32
                topPadding: 6
                text: "No agents running. Start one from + on the tab bar."
                color: theme.overlay0
                font.pixelSize: 12
                wrapMode: Text.Wrap
            }
        }
    }

    function spaceCount() {
        var n = 0
        for (var i = 0; i < spaceModel.count; i++) if (spaceModel.get(i).kind === "space") n++
        return n
    }
    function agentCount() {
        var n = 0
        for (var i = 0; i < agentModel.count; i++) if (agentModel.get(i).kind === "agent") n++
        return n
    }

    // The edge, dragged to make the sidebar wider or narrower.
    Rectangle {
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: 1
        color: theme.crust
    }
    MouseArea {
        anchors.right: parent.right
        anchors.rightMargin: -3
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: 6
        cursorShape: Qt.SplitHCursor
        property real startX: 0
        property real startWidth: 0
        onPressed: (m) => { startX = mapToItem(root.contentItem, m.x, 0).x; startWidth = bar.wanted }
        onPositionChanged: (m) => {
            if (!pressed) return
            var x = mapToItem(root.contentItem, m.x, 0).x
            bar.wanted = Math.max(220, Math.min(460, startWidth + x - startX))
        }
    }

    // ---- the rows ---------------------------------------------------------

    Component {
        id: spaceCard
        Rectangle {
            id: card
            width: parent ? parent.width : 0
            readonly property var r: parent ? parent.row : null
            readonly property var c: r ? JSON.parse(r.space || "null") : null
            readonly property bool active: r ? r.active : false
            height: 54
            color: "transparent"

            Rectangle {
                anchors.fill: parent
                anchors.leftMargin: 8 + (card.r ? card.r.depth : 0) * 12
                anchors.rightMargin: 8
                anchors.topMargin: 2
                anchors.bottomMargin: 2
                radius: 8
                color: card.active ? Qt.rgba(theme.accent.r, theme.accent.g, theme.accent.b, 0.14)
                     : spaceArea.containsMouse ? theme.surface0 : "transparent"
                border.width: card.active ? 1 : 0
                border.color: Qt.rgba(theme.accent.r, theme.accent.g, theme.accent.b, 0.35)

                Avatar {
                    id: spaceAvatar
                    x: 10
                    anchors.verticalCenter: parent.verticalCenter
                    letter: card.c ? card.c.name : "?"
                    tint: root.tintOf(card.c ? card.c.name : "")
                }
                Column {
                    anchors.left: spaceAvatar.right
                    anchors.leftMargin: 10
                    anchors.right: pips.left
                    anchors.rightMargin: 6
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 3
                    Text {
                        width: parent.width
                        text: card.c ? card.c.name : (card.r ? card.r.label : "")
                        color: theme.text
                        font.pixelSize: 13
                        font.weight: card.active ? Font.DemiBold : Font.Medium
                        elide: Text.ElideRight
                    }
                    Row {
                        spacing: 6
                        width: parent.width
                        Text {
                            id: branchText
                            visible: !!(card.c && card.c.branch)
                            text: card.c ? "⎇ " + card.c.branch : ""
                            color: theme.subtext
                            font.pixelSize: 11
                            elide: Text.ElideRight
                            width: Math.min(implicitWidth, parent.width - (sync.visible ? sync.width + 6 : 0))
                        }
                        Chip {
                            id: sync
                            visible: !!(card.c && (card.c.ahead || card.c.behind))
                            text: card.c ? ((card.c.ahead ? "↑" + card.c.ahead : "") + (card.c.ahead && card.c.behind ? " " : "") + (card.c.behind ? "↓" + card.c.behind : "")) : ""
                            tint: theme.peach
                        }
                        Text {
                            visible: !branchText.visible && !!(card.c && card.c.folder)
                            width: parent.width
                            text: card.c ? card.c.folder || "" : ""
                            color: theme.overlay0
                            font.pixelSize: 11
                            elide: Text.ElideMiddle
                        }
                    }
                }
                // How its agents are: one dot per state that has any, with
                // how many.
                Row {
                    id: pips
                    anchors.right: parent.right
                    anchors.rightMargin: 10
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 6
                    Pip { n: card.c ? card.c.waiting || 0 : 0; tint: theme.red; tip: "need you" }
                    Pip { n: card.c ? card.c.working || 0 : 0; tint: theme.yellow; tip: "working" }
                    Pip { n: card.c ? card.c.done || 0 : 0; tint: theme.teal; tip: "done" }
                }
                MouseArea {
                    id: spaceArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    acceptedButtons: Qt.LeftButton | Qt.RightButton
                    onClicked: (m) => {
                        if (!card.r) return
                        if (m.button === Qt.RightButton) {
                            var p = mapToItem(root.contentItem, m.x, m.y)
                            Bridge.call("menu.open", { kind: "space", workspace: card.r.workspace, group: card.r.group,
                                                       x: Math.round(p.x), y: Math.round(p.y) })
                        } else if (card.r.workspace) Bridge.call("workspace", { workspace: card.r.workspace })
                    }
                }
            }
        }
    }

    Component {
        id: groupRow
        Item {
            id: g
            width: parent ? parent.width : 0
            readonly property var r: parent ? parent.row : null
            height: 30
            Rectangle {
                anchors.fill: parent
                anchors.leftMargin: 8
                anchors.rightMargin: 8
                radius: 6
                color: groupArea.containsMouse ? theme.surface0 : "transparent"
                Text {
                    id: chevron
                    x: 10
                    anchors.verticalCenter: parent.verticalCenter
                    text: "›"
                    rotation: g.r && g.r.folded ? 0 : 90
                    color: theme.overlay0
                    font.pixelSize: 15
                    Behavior on rotation { NumberAnimation { duration: 120 } }
                }
                Text {
                    anchors.left: chevron.right
                    anchors.leftMargin: 10
                    anchors.right: parent.right
                    anchors.rightMargin: 10
                    anchors.verticalCenter: parent.verticalCenter
                    text: g.r ? g.r.label : ""
                    color: theme.subtext
                    font.pixelSize: 12
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                }
                MouseArea {
                    id: groupArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    acceptedButtons: Qt.LeftButton | Qt.RightButton
                    onClicked: (m) => {
                        if (!g.r) return
                        if (m.button === Qt.RightButton) {
                            var p = mapToItem(root.contentItem, m.x, m.y)
                            Bridge.call("menu.open", { kind: "group", group: g.r.group, x: Math.round(p.x), y: Math.round(p.y) })
                        } else Bridge.call("group.toggle", { group: g.r.group })
                    }
                }
            }
        }
    }

    Component {
        id: tabHeading
        Item {
            readonly property var r: parent ? parent.row : null
            height: 26
            width: parent ? parent.width : 0
            Text {
                x: 18
                anchors.bottom: parent.bottom
                anchors.bottomMargin: 4
                width: parent.width - 36
                text: r ? r.label : ""
                color: theme.overlay0
                font.pixelSize: 11
                font.weight: Font.DemiBold
                elide: Text.ElideRight
            }
        }
    }

    Component {
        id: agentCard
        Rectangle {
            id: ac
            width: parent ? parent.width : 0
            readonly property var r: parent ? parent.row : null
            readonly property var c: r ? JSON.parse(r.agentCard || "null") : null
            readonly property string state: r ? r.rstate : ""
            readonly property bool active: r ? r.active : false
            height: (c && c.doing) ? 62 : 46
            color: "transparent"

            Rectangle {
                anchors.fill: parent
                anchors.leftMargin: 8 + (ac.r ? ac.r.depth : 0) * 8
                anchors.rightMargin: 8
                anchors.topMargin: 2
                anchors.bottomMargin: 2
                radius: 8
                color: ac.active ? theme.surface0 : agentArea.containsMouse ? Qt.rgba(1, 1, 1, 0.04) : "transparent"
                // Waiting for the person: the card says so before its words do.
                border.width: ac.state === "blocked" ? 1 : 0
                border.color: Qt.rgba(theme.red.r, theme.red.g, theme.red.b, 0.5)

                Avatar {
                    id: agentAvatar
                    x: 10
                    y: 9
                    round: true
                    letter: ac.c ? ac.c.name : "?"
                    tint: root.agentTint(ac.c ? ac.c.kind : "")
                    // Its state, as a dot on the avatar's corner.
                    Rectangle {
                        anchors.right: parent.right
                        anchors.bottom: parent.bottom
                        anchors.rightMargin: -2
                        anchors.bottomMargin: -2
                        width: 11
                        height: 11
                        radius: 6
                        color: theme.stateColor(ac.state, ac.r ? ac.r.running : false)
                        border.width: 2
                        border.color: theme.mantle
                        SequentialAnimation on opacity {
                            running: ac.state === "working"
                            loops: Animation.Infinite
                            NumberAnimation { to: 0.35; duration: 700; easing.type: Easing.InOutQuad }
                            NumberAnimation { to: 1; duration: 700; easing.type: Easing.InOutQuad }
                            onRunningChanged: if (!running) parent.opacity = 1
                        }
                    }
                }
                Column {
                    anchors.left: agentAvatar.right
                    anchors.leftMargin: 10
                    anchors.right: parent.right
                    anchors.rightMargin: 10
                    y: 7
                    spacing: 2
                    Item {
                        width: parent.width
                        height: nameText.height
                        Text {
                            id: nameText
                            anchors.left: parent.left
                            anchors.right: stateText.left
                            anchors.rightMargin: 6
                            text: ac.c ? ac.c.name : (ac.r ? ac.r.label : "")
                            color: theme.text
                            font.pixelSize: 13
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }
                        Text {
                            id: stateText
                            anchors.right: parent.right
                            anchors.verticalCenter: nameText.verticalCenter
                            text: ac.c ? ac.c.stateText || "" : ""
                            color: theme.stateColor(ac.state, ac.r ? ac.r.running : false)
                            font.pixelSize: 11
                            font.weight: ac.state === "blocked" ? Font.DemiBold : Font.Normal
                        }
                    }
                    Text {
                        visible: !!(ac.c && ac.c.doing)
                        width: parent.width
                        text: ac.c ? ac.c.doing || "" : ""
                        color: theme.subtext
                        font.pixelSize: 12
                        elide: Text.ElideRight
                    }
                    Item {
                        width: parent.width
                        height: whereText.height
                        Text {
                            id: whereText
                            anchors.left: parent.left
                            anchors.right: usage.left
                            anchors.rightMargin: 6
                            text: ac.c ? ac.c.where || "" : ""
                            color: theme.overlay0
                            font.pixelSize: 11
                            elide: Text.ElideRight
                        }
                        Row {
                            id: usage
                            anchors.right: parent.right
                            anchors.verticalCenter: whereText.verticalCenter
                            spacing: 6
                            // How full its context is, as a small bar.
                            Rectangle {
                                visible: !!(ac.c && ac.c.context >= 0)
                                anchors.verticalCenter: parent.verticalCenter
                                width: 28
                                height: 4
                                radius: 2
                                color: theme.surface1
                                Rectangle {
                                    width: parent.width * Math.min(1, (ac.c ? ac.c.context : 0) / 100)
                                    height: parent.height
                                    radius: 2
                                    color: ac.c && ac.c.contextLevel === 2 ? theme.red : ac.c && ac.c.contextLevel === 1 ? theme.yellow : theme.accent
                                }
                            }
                            Text {
                                visible: !!(ac.c && ac.c.cost)
                                text: ac.c ? ac.c.cost || "" : ""
                                color: theme.overlay0
                                font.pixelSize: 11
                            }
                        }
                    }
                }
                MouseArea {
                    id: agentArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    acceptedButtons: Qt.LeftButton | Qt.RightButton
                    onClicked: (m) => {
                        if (!ac.r) return
                        if (m.button === Qt.RightButton) {
                            var p = mapToItem(root.contentItem, m.x, m.y)
                            Bridge.call("menu.open", { kind: "agent", workspace: ac.r.workspace, pane: ac.r.pane, tab: ac.r.tab,
                                                       x: Math.round(p.x), y: Math.round(p.y) })
                        } else if (ac.r.pane) Bridge.call("jump", { pane: ac.r.pane })
                    }
                }
            }
        }
    }

    // ---- small parts ------------------------------------------------------

    component SectionTitle: Item {
        property alias text: label.text
        property int count: 0
        width: parent ? parent.width : 0
        height: 30
        Text {
            id: label
            x: 16
            anchors.verticalCenter: parent.verticalCenter
            color: theme.subtext
            font.pixelSize: 12
            font.weight: Font.DemiBold
        }
        Text {
            anchors.left: label.right
            anchors.leftMargin: 6
            anchors.verticalCenter: parent.verticalCenter
            visible: parent.count > 0
            text: parent.count
            color: theme.overlay0
            font.pixelSize: 11
        }
    }

    component Avatar: Rectangle {
        property string letter: ""
        property color tint: theme.accent
        property bool round: false
        width: 30
        height: 30
        radius: round ? 15 : 8
        color: Qt.rgba(tint.r, tint.g, tint.b, 0.2)
        border.width: 1
        border.color: Qt.rgba(tint.r, tint.g, tint.b, 0.45)
        Text {
            anchors.centerIn: parent
            text: (parent.letter.replace(/^[^A-Za-z0-9]+/, "").charAt(0) || "?").toUpperCase()
            color: parent.tint
            font.pixelSize: 13
            font.bold: true
        }
    }

    component Chip: Rectangle {
        property alias text: chipText.text
        property color tint: theme.accent
        width: chipText.implicitWidth + 10
        height: 16
        radius: 8
        color: Qt.rgba(tint.r, tint.g, tint.b, 0.16)
        Text {
            id: chipText
            anchors.centerIn: parent
            color: parent.tint
            font.pixelSize: 10
            font.weight: Font.DemiBold
        }
    }

    component Pip: Row {
        property int n: 0
        property color tint: theme.text
        property string tip: ""
        visible: n > 0
        spacing: 3
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            width: 7
            height: 7
            radius: 4
            color: parent.tint
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: parent.n
            color: theme.subtext
            font.pixelSize: 11
        }
    }

    component IconButton: Rectangle {
        id: ib
        property string glyph: ""
        property string tip: ""
        property bool dot: false
        signal clicked()
        width: 28
        height: 28
        radius: 7
        color: ibArea.containsMouse ? theme.surface0 : "transparent"
        Text {
            anchors.centerIn: parent
            text: ib.glyph
            color: ibArea.containsMouse ? theme.text : theme.subtext
            font.pixelSize: 16
        }
        Rectangle {
            visible: ib.dot
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: 5
            width: 7
            height: 7
            radius: 4
            color: theme.accent
        }
        MouseArea {
            id: ibArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: ib.clicked()
        }
    }

    component Segmented: Rectangle {
        id: seg
        property string leftText: ""
        property string rightText: ""
        property bool rightOn: false
        signal picked(bool right)
        width: leftPart.width + rightPart.width + 4
        height: 22
        radius: 6
        color: theme.crust
        Row {
            x: 2
            y: 2
            Rectangle {
                id: leftPart
                width: lt.implicitWidth + 14
                height: 18
                radius: 5
                color: !seg.rightOn ? theme.surface0 : "transparent"
                Text { id: lt; anchors.centerIn: parent; text: seg.leftText; font.pixelSize: 11; color: !seg.rightOn ? theme.text : theme.overlay0 }
                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: seg.picked(false) }
            }
            Rectangle {
                id: rightPart
                width: rt.implicitWidth + 14
                height: 18
                radius: 5
                color: seg.rightOn ? theme.surface0 : "transparent"
                Text { id: rt; anchors.centerIn: parent; text: seg.rightText; font.pixelSize: 11; color: seg.rightOn ? theme.text : theme.overlay0 }
                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: seg.picked(true) }
            }
        }
    }
}
