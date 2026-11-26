// The dock: the window's right side, one tab at a time, beside a rail of
// the tabs there are. Each tab is a file in panels/, made only while it is
// the one open; its data comes from Go (st.dock) when it opens and when the
// server says it changed.
import QtQuick
import Tend 1.0

Item {
    id: dock
    readonly property string tab: (st.dock && st.dock.tab) || ""
    readonly property bool open: tab !== ""
    // Hidden (a window kept on one tab), it takes no width either.
    width: visible ? rail.width + (open ? panelWidth : 0) : 0
    property real panelWidth: 380

    // The tabs, as a rail of marks along the window's edge.
    readonly property var tabs: [
        { name: "files", mark: "❐", tip: "files" },
        { name: "tasks", mark: "✓", tip: "tasks" },
        { name: "whatsapp", mark: "✆", tip: "WhatsApp" }
    ]

    Rectangle {
        id: rail
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: 40
        color: theme.mantle
        Column {
            y: 8
            width: parent.width
            spacing: 4
            Repeater {
                model: dock.tabs
                delegate: Rectangle {
                    required property var modelData
                    readonly property bool chosen: dock.tab === modelData.name
                    x: 4
                    width: 32
                    height: 32
                    radius: 7
                    color: chosen ? theme.surface0 : (markArea.containsMouse ? Qt.rgba(1, 1, 1, 0.05) : "transparent")
                    Text {
                        anchors.centerIn: parent
                        text: modelData.mark
                        color: chosen ? theme.accent : theme.subtext
                        font.pixelSize: 16
                    }
                    MouseArea {
                        id: markArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: Bridge.call("dock", { name: modelData.name })
                    }
                }
            }
        }
        Rectangle { anchors.left: parent.left; width: 1; height: parent.height; color: theme.crust }
    }

    Rectangle {
        visible: dock.open
        anchors.left: parent.left
        anchors.right: rail.left
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        color: theme.mantle
        Rectangle { anchors.left: parent.left; width: 1; height: parent.height; color: theme.crust }

        // Its left edge is dragged to resize it.
        MouseArea {
            width: 6
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            cursorShape: Qt.SplitHCursor
            property real startX: 0
            property real startWidth: 0
            onPressed: (m) => { startX = mapToItem(root.contentItem, m.x, 0).x; startWidth = dock.panelWidth }
            onPositionChanged: (m) => {
                if (!pressed) return
                var x = mapToItem(root.contentItem, m.x, 0).x
                dock.panelWidth = Math.max(260, Math.min(root.width * 0.6, startWidth - (x - startX)))
            }
        }

        Loader {
            anchors.fill: parent
            anchors.margins: 14
            active: dock.open
            source: dock.open ? "panels/" + dock.tab.charAt(0).toUpperCase() + dock.tab.slice(1) + ".qml" : ""
        }
        Text {
            visible: !!(st.dock && st.dock.error)
            anchors.bottom: parent.bottom
            anchors.margins: 12
            x: 14
            width: parent.width - 28
            text: (st.dock && st.dock.error) || ""
            color: theme.red
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
    }
}
