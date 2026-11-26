// The desktop window. For now it is one pane, the one the session has in
// focus, filling the window; the sidebar and tabs come next (docs/DESKTOP.md).
import QtQuick
import QtQuick.Window
import Tend 1.0

Window {
    id: root
    visible: true
    width: 1100
    height: 700
    color: "#1e1e2e"
    title: "tend — " + session

    // Set by Go: the session's name, the pane to show, and a line for the
    // user when something went wrong.
    property string session: ""
    property var pane: 0
    property string status: ""

    Terminal {
        id: term
        anchors.fill: parent
        anchors.margins: 6
        anchors.bottomMargin: statusBar.visible ? statusBar.height + 6 : 6
        pane: root.pane
        focus: true
        Component.onCompleted: forceActiveFocus()
    }

    Rectangle {
        id: statusBar
        visible: root.status !== ""
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        height: 24
        color: "#181825"
        Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.leftMargin: 8
            text: root.status
            color: "#f38ba8"
        }
    }
}
