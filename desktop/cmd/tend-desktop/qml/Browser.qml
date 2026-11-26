// A web page in a tab of the window, as Orca has one beside its terminals.
//
// This file is the only one that imports QtWebEngine, and it is not loaded
// with the window: main.qml makes one with Qt.createQmlObject when a browser
// tab is opened (docs/DESKTOP.md, rule 1). Until then the window has no
// Chromium in it, and it costs what a terminal costs.
import QtQuick
import QtWebEngine
import Tend 1.0

Rectangle {
    id: browser
    color: "#1e1e2e"

    // What the tab says, and the address shown; main.qml reads them.
    property string url: ""
    readonly property string pageTitle: view.title
    readonly property bool loading: view.loading

    // The window's theme (main.qml's theme object), which this page, made
    // in main.qml's context, sees as its own.

    // Element picking, as tend's extension does it (content.js): the
    // element under the pointer outlined, a click capturing it — its CSS
    // selector, tag, text and attributes — and handed to the session's
    // context buffer. The page says what it picked on its console, which
    // only this view reads.
    property bool picking: false
    readonly property string pickScript: "(function(on){" +
        "var w=window; if(w.__tendPick){w.__tendPick(false)}; if(!on)return;" +
        "var box=document.createElement('div');box.style.cssText='position:fixed;pointer-events:none;z-index:2147483647;border:2px solid #89b4fa;background:rgba(137,180,250,.15)';" +
        "document.documentElement.appendChild(box);" +
        "function uniq(s){try{return document.querySelectorAll(s).length===1}catch(e){return false}}" +
        "function sel(el){if(el.id&&uniq('#'+CSS.escape(el.id)))return '#'+CSS.escape(el.id);var p=[],n=el;" +
        "while(n&&n.nodeType===1&&n!==document.documentElement){if(n!==el&&n.id&&uniq('#'+CSS.escape(n.id))){p.unshift('#'+CSS.escape(n.id));break}" +
        "var t=n.tagName.toLowerCase(),c=[].slice.call(n.classList).filter(function(x){return /^[a-zA-Z_-][\\w-]*$/.test(x)}).slice(0,2);" +
        "if(c.length)t+='.'+c.map(function(x){return CSS.escape(x)}).join('.');var par=n.parentElement;" +
        "if(par){var same=[].filter.call(par.children,function(x){return x.tagName===n.tagName});if(same.length>1)t+=':nth-of-type('+(same.indexOf(n)+1)+')'}" +
        "p.unshift(t);var s=p.join(' > ');if(uniq(s))return s;n=par}return p.join(' > ')}" +
        "function move(e){var r=e.target.getBoundingClientRect();box.style.left=r.left+'px';box.style.top=r.top+'px';box.style.width=r.width+'px';box.style.height=r.height+'px'}" +
        "function click(e){e.preventDefault();e.stopPropagation();var el=e.target,a={};" +
        "[].slice.call(el.attributes,0,24).forEach(function(x){a[x.name]=x.value.length>200?x.value.slice(0,199)+'…':x.value});" +
        "var tx=(el.innerText||el.value||el.getAttribute('aria-label')||el.getAttribute('alt')||'').trim();if(tx.length>2000)tx=tx.slice(0,1999)+'…';" +
        "console.log('tend-pick:'+JSON.stringify({url:location.href,title:document.title,selector:sel(el),tag:el.tagName.toLowerCase(),text:tx,attributes:a}))}" +
        "document.addEventListener('mouseover',move,true);document.addEventListener('click',click,true);" +
        "w.__tendPick=function(){document.removeEventListener('mouseover',move,true);document.removeEventListener('click',click,true);box.remove();w.__tendPick=null}" +
        "})"
    function setPicking(on) {
        picking = on
        view.runJavaScript(pickScript + "(" + (on ? "true" : "false") + ")")
    }

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
        onJavaScriptConsoleMessage: (level, message, line, source) => {
            if (message.indexOf("tend-pick:") === 0)
                Bridge.call("browser.picked", JSON.parse(message.slice(10)))
        }
        // Picking stays on across pages, as the extension's does.
        onLoadingChanged: (info) => { if (!view.loading && browser.picking) browser.setPicking(true) }
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
