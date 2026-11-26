// A line of text typed on a tools' screen: a server to add, a rule, a
// search. Enter says it is done.
import QtQuick

Rectangle {
    id: field
    property alias text: input.text
    property string hint: ""
    signal accepted()
    signal edited()
    height: 30
    radius: 6
    color: theme.base
    border.color: input.activeFocus ? theme.accent : theme.surface1
    function focusIt() { input.forceActiveFocus() }
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
        onAccepted: field.accepted()
        onTextEdited: field.edited()
    }
    Text {
        visible: input.text === "" && !input.activeFocus
        anchors.left: parent.left
        anchors.leftMargin: 8
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        text: field.hint
        color: theme.overlay0
        font.pixelSize: 12
        elide: Text.ElideRight
    }
}
