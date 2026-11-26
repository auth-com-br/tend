// A chip of the issues, errors and tickets tabs: a preset, a scope, a
// mode, or a button — chosen or not, plain, primary or a warning. One file
// so the three tabs look and answer alike.
import QtQuick

Rectangle {
    id: chip
    property string label: ""
    property bool chosen: false
    property bool primary: false
    property bool danger: false
    property bool enabled: true
    signal clicked()
    height: 24
    width: label_.width + 16
    radius: 5
    opacity: enabled ? 1 : 0.45
    color: chosen || primary ? (danger ? theme.red : theme.accent)
                             : (area.containsMouse && enabled ? theme.surface1 : theme.surface0)
    Text {
        id: label_
        anchors.centerIn: parent
        text: chip.label
        font.pixelSize: 11
        font.bold: chip.primary
        color: chip.chosen || chip.primary ? theme.base : (chip.danger ? theme.red : theme.text)
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: chip.enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: if (chip.enabled) chip.clicked()
    }
}
