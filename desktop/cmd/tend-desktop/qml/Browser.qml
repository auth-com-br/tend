// A web page in a tab of the window, as Orca has one beside its terminals.
//
// This file is the only one that imports QtWebEngine, and it is not loaded
// with the window: main.qml makes one with Qt.createQmlObject when a browser
// tab is opened (docs/DESKTOP.md, rule 1). Until then the window has no
// Chromium in it, and it costs what a terminal costs.
import QtQuick
import QtWebEngine

Rectangle {
    id: browser
    color: "#1e1e2e"

    // What the tab says, and the address shown; main.qml reads them.
    property string url: ""
    readonly property string pageTitle: view.title
    readonly property bool loading: view.loading

    // The window's theme (main.qml's theme object), which this page, made
    // in main.qml's context, sees as its own.

    function go(text) {
        var t = text.trim()
        if (t === "") return
        // An address as people type one: a scheme is added, and words
        // that are not an address are searched for.
        if (!/^[a-z][a-z0-9+.-]*:/i.test(t)) {
            if (/^[^\s]+\.[^\s]+$/.test(t) || /^localhost(:\d+)?(\/.*)?$/.test(t))
                t = (/^localhost/.test(t) ? "http://" : "https://") + t
            else
                t = "https://duckduckgo.com/?q=" + encodeURIComponent(t)
        }
        view.url = t
    }

    Rectangle {
        id: toolbar
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: 40
        color: theme.mantle

        Row {
            id: nav
            anchors.left: parent.left
            anchors.leftMargin: 6
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2
            NavButton { glyph: "←"; enabled: view.canGoBack; onClicked: view.goBack() }
            NavButton { glyph: "→"; enabled: view.canGoForward; onClicked: view.goForward() }
            NavButton { glyph: view.loading ? "✕" : "↻"; onClicked: view.loading ? view.stop() : view.reload() }
        }

        Rectangle {
            anchors.left: nav.right
            anchors.leftMargin: 8
            anchors.right: external.left
            anchors.rightMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            height: 28
            radius: 6
            color: theme.base
            border.width: address.activeFocus ? 1 : 0
            border.color: theme.accent

            TextInput {
                id: address
                anchors.fill: parent
                anchors.leftMargin: 10
                anchors.rightMargin: 10
                verticalAlignment: TextInput.AlignVCenter
                color: theme.text
                selectionColor: theme.surface1
                selectByMouse: true
                clip: true
                font.pixelSize: 14
                text: view.url.toString()
                onAccepted: { browser.go(text); view.forceActiveFocus() }
                onActiveFocusChanged: if (activeFocus) selectAll()
            }
            Text {
                visible: address.text === "" && !address.activeFocus
                anchors.left: parent.left
                anchors.leftMargin: 10
                anchors.verticalCenter: parent.verticalCenter
                text: "address or search"
                color: theme.overlay0
                font.pixelSize: 14
            }
        }

        NavButton {
            id: external
            anchors.right: parent.right
            anchors.rightMargin: 6
            anchors.verticalCenter: parent.verticalCenter
            glyph: "↗"
            onClicked: Qt.openUrlExternally(view.url)
        }

        // How far the page has loaded, along the toolbar's foot.
        Rectangle {
            visible: view.loading
            anchors.left: parent.left
            anchors.bottom: parent.bottom
            height: 2
            width: parent.width * view.loadProgress / 100
            color: theme.accent
        }
    }

    WebEngineProfilePrototype {
        id: profileProto
        storageName: "tend-desktop"
    }

    WebEngineView {
        id: view
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: toolbar.bottom
        anchors.bottom: parent.bottom
        // One profile for the window, kept between runs, so a sign-in
        // survives closing the tab: an agent's dashboard stays signed in.
        // Made from a prototype, Qt 6.9's way: a WebEngineProfile given its
        // storage name as a property was made off the record first and
        // moved to disk after, with a warning for each tab.
        profile: profileProto.instance()
        onNewWindowRequested: (request) => { view.url = request.requestedUrl }
    }

    component NavButton: Rectangle {
        id: nb
        property string glyph: ""
        property bool enabled: true
        signal clicked()
        width: 30
        height: 28
        radius: 5
        color: nbArea.containsMouse && nb.enabled ? theme.surface0 : "transparent"
        Text {
            anchors.centerIn: parent
            text: nb.glyph
            color: nb.enabled ? theme.text : theme.surface1
            font.pixelSize: 15
        }
        MouseArea {
            id: nbArea
            anchors.fill: parent
            hoverEnabled: true
            enabled: nb.enabled
            cursorShape: Qt.PointingHandCursor
            onClicked: nb.clicked()
        }
    }

    Component.onCompleted: address.forceActiveFocus()
}
