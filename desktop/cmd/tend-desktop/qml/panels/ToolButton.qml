// A button of the tools' screens.
import QtQuick

Rectangle {
    id: button
    property string label: ""
    property bool primary: false
    property bool danger: false
    property bool active: true
    signal clicked()
    height: 26
    width: labelText.width + 20
    radius: 6
    opacity: active ? 1 : 0.4
    color: primary ? theme.accent : (area.containsMouse && button.active ? theme.surface1 : theme.surface0)
    Text {
        id: labelText
        anchors.centerIn: parent
        text: button.label
        color: button.primary ? theme.base : (button.danger ? theme.red : theme.text)
        font.pixelSize: 12
        font.bold: button.primary
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: button.active ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: if (button.active) button.clicked()
    }
}
