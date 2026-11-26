// What's new: the release notes the server read — of the release running,
// or of a newer one ready to install — as the TUI's release notes panel
// shows them (herdr's), the markdown drawn by Qt's own. Closing it tells
// the server they were read.
import QtQuick
import Tend 1.0

ToolFrame {
    id: screen
    readonly property var s: st.notes || ({ loading: true })
    title: s.newer ? "Update ready" : "What's new"
    subtitle: s.version ? (s.newer ? "v" + s.version + " is out" : "in v" + s.version) : ""
    message: s.error || ""
    alert: !!s.error
    loading: !!s.loading
    boxWidth: 760
    onClosed: Bridge.call("notes.close", {})

    Text {
        id: install
        visible: !!screen.s.newer && !!screen.s.install
        width: parent.width
        text: "install it with: " + (screen.s.install || "")
        color: theme.accent
        font.family: "monospace"
        font.pixelSize: 12
        wrapMode: Text.Wrap
    }
    Flickable {
        y: install.visible ? install.height + 10 : 0
        width: parent.width
        height: parent.height - y
        contentHeight: body.height
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        Text {
            id: body
            width: parent.width
            text: screen.s.body || ""
            textFormat: Text.MarkdownText
            color: theme.text
            linkColor: theme.accent
            font.pixelSize: 13
            wrapMode: Text.Wrap
            onLinkActivated: (link) => Qt.openUrlExternally(link)
        }
    }
}
