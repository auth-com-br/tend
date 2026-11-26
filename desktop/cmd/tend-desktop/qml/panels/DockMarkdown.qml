// Markdown as Qt renders it (Text.MarkdownText, docs/DESKTOP.md rule 1: no
// browser engine), for an issue's body, a comment, a ticket. Links open in
// the desktop's browser.
import QtQuick

Text {
    property string source: ""
    width: parent ? parent.width : 0
    text: source
    textFormat: Text.MarkdownText
    wrapMode: Text.Wrap
    color: theme.text
    linkColor: theme.accent
    font.pixelSize: 13
    onLinkActivated: (link) => Qt.openUrlExternally(link)
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.NoButton
        cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor
    }
}
