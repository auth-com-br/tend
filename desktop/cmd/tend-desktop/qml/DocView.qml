// A document tab: a file shown read-only — code with the highlighter's
// tokens, markdown rendered by Qt, an image — or a change's diff side by
// side, where a click on a line asks for a comment that goes to the tab's
// agent (the files panel's review, #71). Nothing here draws through a
// browser engine.
import QtQuick
import Tend 1.0

Rectangle {
    id: view
    color: theme.base
    readonly property var d: st.doc || ({})
    readonly property real mono: 13
    function tokenColor(k) {
        switch (k) {
        case 1: return theme.peach   // keyword
        case 2: return theme.green   // string
        case 3: return theme.overlay0 // comment
        case 4: return theme.yellow  // number
        case 5: return theme.accent  // heading
        }
        return theme.text
    }
    // A token's text made safe for rich text, its tabs and spaces kept: done
    // to the text alone — done to the whole line, it broke the span tags'
    // own spaces and every line came out uncoloured.
    function escapeHtml(s) {
        return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
                .replace(/\t/g, "    ").replace(/ /g, "&nbsp;")
    }

    Rectangle {
        id: bar
        width: parent.width
        height: 30
        color: theme.mantle
        Text {
            x: 12
            anchors.verticalCenter: parent.verticalCenter
            width: parent.width - 24
            text: (view.d.path || "") + (view.d.truncated ? "  (cut short)" : "")
            color: theme.subtext
            font.pixelSize: 12
            elide: Text.ElideMiddle
        }
    }

    // Code: a line a row, numbered, coloured by token kind.
    ListView {
        id: code
        visible: view.d.kind === "file" && view.d.view === "text"
        anchors.top: bar.bottom
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        model: view.d.lines || []
        boundsBehavior: Flickable.StopAtBounds
        Component.onCompleted: if (view.d.line > 0) positionViewAtIndex(view.d.line - 1, ListView.Center)
        delegate: Row {
            required property var modelData
            required property int index
            height: 19
            Text {
                width: 56
                rightPadding: 12
                horizontalAlignment: Text.AlignRight
                text: index + 1
                color: index + 1 === view.d.line ? theme.accent : theme.overlay0
                font.family: "monospace"
                font.pixelSize: view.mono - 1
            }
            Text {
                textFormat: Text.RichText
                text: modelData.map(function (t) { return "<span style='color:" + view.tokenColor(t.k || 0) + "'>" + view.escapeHtml(t.t) + "</span>" }).join("")
                font.family: "monospace"
                font.pixelSize: view.mono
            }
        }
    }

    // Markdown, rendered by Qt itself.
    Flickable {
        visible: view.d.kind === "file" && view.d.view === "markdown"
        anchors.top: bar.bottom
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        contentHeight: md.height + 40
        Text {
            id: md
            x: 32
            y: 20
            width: Math.min(parent.width - 64, 900)
            textFormat: Text.MarkdownText
            text: view.d.source || ""
            color: theme.text
            linkColor: theme.accent
            wrapMode: Text.Wrap
            font.pixelSize: 15
            onLinkActivated: (link) => Qt.openUrlExternally(link)
        }
    }

    Image {
        visible: view.d.kind === "file" && view.d.view === "image"
        anchors.top: bar.bottom
        anchors.bottom: parent.bottom
        anchors.margins: 20
        width: parent.width - 40
        x: 20
        fillMode: Image.PreserveAspectFit
        source: view.d.view === "image" ? "file://" + view.d.path : ""
        asynchronous: true
    }

    Text {
        visible: view.d.view === "binary" || (view.d.kind === "diff" && !!view.d.binary)
        anchors.centerIn: parent
        text: "a binary file: nothing to show as text"
        color: theme.overlay0
    }

    // A diff, side by side: the old file left, the new right.
    ListView {
        id: diff
        visible: view.d.kind === "diff"
        anchors.top: bar.bottom
        anchors.bottom: parent.bottom
        width: parent.width
        clip: true
        model: view.d.hunks || []
        delegate: Column {
            required property var modelData
            width: diff.width
            Rectangle {
                width: parent.width
                height: 22
                color: theme.surface0
                Text { x: 10; anchors.verticalCenter: parent.verticalCenter; text: modelData.header; color: theme.subtext; font.family: "monospace"; font.pixelSize: 11 }
            }
            Repeater {
                model: modelData.rows
                delegate: Row {
                    required property var modelData
                    width: diff.width
                    height: 19
                    Side { num: modelData.on || 0; text: modelData.o || ""; kind: modelData.ok || 0; width: diff.width / 2; line: 0 }
                    Side { num: modelData.nn || 0; text: modelData.n || ""; kind: modelData.nk || 0; width: diff.width / 2; line: modelData.nn || 0 }
                }
            }
        }
    }

    component Side: Rectangle {
        id: side
        property int num
        property string text
        property int kind
        property int line
        height: 19
        color: kind === 1 ? Qt.rgba(0.3, 0.8, 0.4, 0.15) : kind === 2 ? Qt.rgba(0.95, 0.3, 0.4, 0.15) : "transparent"
        Text {
            width: 48
            rightPadding: 8
            horizontalAlignment: Text.AlignRight
            text: side.num > 0 ? side.num : ""
            color: theme.overlay0
            font.family: "monospace"
            font.pixelSize: 11
            anchors.verticalCenter: parent.verticalCenter
        }
        Text {
            x: 52
            width: parent.width - 56
            anchors.verticalCenter: parent.verticalCenter
            text: side.text.replace(/\t/g, "    ")
            color: theme.text
            font.family: "monospace"
            font.pixelSize: 12
            elide: Text.ElideRight
        }
        // A line of the new file takes a review comment for the agent.
        MouseArea {
            anchors.fill: parent
            enabled: side.line > 0
            cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
            onClicked: { reviewBox.line = side.line; reviewBox.visible = true; reviewInput.forceActiveFocus() }
        }
    }

    // The comment, as the files panel's r asks it.
    Rectangle {
        id: reviewBox
        property int line: 0
        visible: false
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 20
        width: Math.min(parent.width - 40, 640)
        height: 84
        radius: 8
        color: theme.mantle
        border.color: theme.accent
        Text { x: 12; y: 8; text: "review of " + (view.d.path || "") + " around line " + reviewBox.line + " — to the tab's agent"; color: theme.subtext; font.pixelSize: 11 }
        Rectangle {
            x: 10
            y: 30
            width: parent.width - 20
            height: 42
            radius: 6
            color: theme.base
            TextInput {
                id: reviewInput
                anchors.fill: parent
                anchors.margins: 8
                verticalAlignment: TextInput.AlignVCenter
                color: theme.text
                font.pixelSize: 13
                onAccepted: {
                    Bridge.call("files", { do: "review", path: view.d.path, line: reviewBox.line, text: text })
                    text = ""
                    reviewBox.visible = false
                }
                Keys.onEscapePressed: reviewBox.visible = false
            }
        }
    }
}
