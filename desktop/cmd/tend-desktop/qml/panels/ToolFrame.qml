// The frame the global menu's tools are drawn in: a dimmed window, a box
// with a title, a close mark, the line of what happened, and a question
// waiting for yes or no. Each tool's screen puts its own content in it.
// Esc answers no to a question first, then closes; a click outside closes.
import QtQuick
import Tend 1.0

Rectangle {
    id: frame
    color: Qt.rgba(0, 0, 0, 0.5)
    property string title: ""
    property string subtitle: ""
    property string message: ""
    property bool alert: false
    property bool loading: false
    property string confirm: ""
    property int boxWidth: 860
    property int boxHeight: 600
    // Detail, when set, is shown over the content with a way back.
    property var detail: []
    signal answered(bool yes)
    signal closed()
    signal detailClosed()
    default property alias content: body.data
    readonly property alias body: body

    function close() { if (frame.confirm !== "") frame.answered(false); else frame.closed() }

    MouseArea { anchors.fill: parent; onPressed: frame.closed() }
    Keys.onEscapePressed: frame.close()
    Component.onCompleted: forceActiveFocus()

    Rectangle {
        id: box
        anchors.centerIn: parent
        width: Math.min(parent.width - 60, frame.boxWidth)
        height: Math.min(parent.height - 60, frame.boxHeight)
        radius: 10
        color: theme.mantle
        border.color: theme.surface1
        MouseArea { anchors.fill: parent }

        Text {
            id: titleText
            x: 20
            y: 14
            text: frame.title
            color: theme.text
            font.pixelSize: 18
            font.bold: true
        }
        Text {
            anchors.left: titleText.right
            anchors.leftMargin: 12
            anchors.baseline: titleText.baseline
            anchors.right: closeMark.left
            anchors.rightMargin: 12
            text: frame.subtitle
            color: theme.overlay0
            font.pixelSize: 12
            elide: Text.ElideMiddle
        }
        Text {
            id: closeMark
            anchors.right: parent.right
            anchors.rightMargin: 16
            y: 14
            text: "×"
            color: closeArea.containsMouse ? theme.red : theme.overlay0
            font.pixelSize: 18
            MouseArea {
                id: closeArea
                anchors.fill: parent
                anchors.margins: -6
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: frame.closed()
            }
        }

        Item {
            id: body
            x: 16
            y: 50
            width: parent.width - 32
            height: parent.height - 50 - foot.height - 12
            visible: frame.detail.length === 0
        }

        // A row's details, or the help, in place of the content.
        Rectangle {
            x: 16
            y: 50
            width: parent.width - 32
            height: parent.height - 50 - foot.height - 12
            visible: frame.detail.length > 0
            color: theme.base
            radius: 6
            Flickable {
                anchors.fill: parent
                anchors.margins: 12
                contentHeight: detailText.height
                clip: true
                Text {
                    id: detailText
                    width: parent.width
                    text: frame.detail.join("\n")
                    color: theme.text
                    font.family: "monospace"
                    font.pixelSize: 12
                    wrapMode: Text.Wrap
                }
            }
            ToolButton {
                anchors.right: parent.right
                anchors.bottom: parent.bottom
                anchors.margins: 10
                label: "back"
                onClicked: frame.detailClosed()
            }
        }

        // The line of what happened, or the question.
        Item {
            id: foot
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 10
            x: 16
            width: parent.width - 32
            height: 34
            Text {
                visible: frame.confirm === ""
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width
                text: frame.loading ? "reading…" : frame.message
                color: frame.alert ? theme.red : theme.subtext
                font.pixelSize: 12
                elide: Text.ElideRight
            }
            Rectangle {
                visible: frame.confirm !== ""
                anchors.fill: parent
                radius: 6
                color: theme.surface0
                Text {
                    x: 12
                    anchors.verticalCenter: parent.verticalCenter
                    width: parent.width - 180
                    text: frame.confirm
                    color: theme.text
                    font.pixelSize: 13
                    elide: Text.ElideRight
                }
                Row {
                    anchors.right: parent.right
                    anchors.rightMargin: 6
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 6
                    ToolButton { label: "yes"; primary: true; onClicked: frame.answered(true) }
                    ToolButton { label: "no"; onClicked: frame.answered(false) }
                }
            }
        }
    }
}
