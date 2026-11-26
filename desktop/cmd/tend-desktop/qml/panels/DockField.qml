// A box to type into for the issues, errors and tickets tabs: one line, or
// several (multiline) for a comment or an issue's text. Enter on one line
// is accepted(); several lines take enter as a new line, and ctrl+enter as
// accepted().
import QtQuick

Rectangle {
    id: field
    property alias text: edit.text
    property string hint: ""
    property bool multiline: false
    signal accepted()
    function focusIt() { edit.forceActiveFocus() }
    height: multiline ? Math.max(70, edit.contentHeight + 14) : 30
    radius: 6
    color: theme.base
    border.color: edit.activeFocus ? theme.accent : theme.surface1
    TextEdit {
        id: edit
        anchors.fill: parent
        anchors.margins: 7
        color: theme.text
        font.pixelSize: 13
        wrapMode: field.multiline ? TextEdit.Wrap : TextEdit.NoWrap
        selectByMouse: true
        clip: true
        Keys.onReturnPressed: (e) => {
            if (!field.multiline || (e.modifiers & Qt.ControlModifier)) {
                field.accepted()
                e.accepted = true
            } else {
                e.accepted = false
            }
        }
        Keys.onEnterPressed: (e) => { field.accepted(); e.accepted = true }
    }
    Text {
        visible: edit.text === "" && !edit.activeFocus
        x: 8
        y: 7
        text: field.hint
        color: theme.overlay0
        font.pixelSize: 12
    }
}
