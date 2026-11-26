// The dock's issues: the TUI's issues panel natively (core/issues.go) — the
// project's GitHub or GitLab issues and pull requests, searched with
// GitHub's search syntax, one opened with its thread, commented on, closed
// or reopened, filed, and worked on in a worktree with an agent; a pull
// request with its checks, merged, closed, reopened or made ready.
import QtQuick
import Tend 1.0

Item {
    id: issues
    readonly property var v: (st.dock && st.dock.issues) || ({ filters: [], issues: [], prs: [], scopes: [], remotes: [] })
    readonly property bool detail: !!(v.issue || v.pr)
    property bool composing: false
    property bool commenting: false
    property string asking: ""   // close, merge: the question a button asked
    function call(args) { Bridge.call("issues", args) }
    function checks(p) {
        var out = []
        if (p.pass) out.push("✓" + p.pass)
        if (p.fail) out.push("✗" + p.fail)
        if (p.pending) out.push("●" + p.pending)
        return out.join(" ")
    }
    onDetailChanged: { commenting = false; asking = "" }

    // Typing searches a moment after it stops, as the TUI does: each search
    // is GitHub's, over the network.
    Timer {
        id: searchLater
        interval: 400
        onTriggered: issues.call({ do: "search", query: search.text })
    }

    Column {
        id: head
        width: parent.width
        spacing: 8
        Row {
            width: parent.width
            spacing: 6
            Text {
                text: issues.v.pullRequests ? "Pull requests" : "Issues"
                color: theme.text
                font.pixelSize: 16
                font.bold: true
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: (issues.v.repo || "") + (issues.v.total ? " · " + issues.v.total : "") + (issues.v.busy || issues.v.reading ? " · asking…" : "")
                color: theme.subtext
                font.pixelSize: 12
                elide: Text.ElideRight
                width: parent.width - 220
            }
        }
        Row {
            visible: !issues.detail
            spacing: 4
            DockChip { label: "issues"; chosen: !issues.v.pullRequests; onClicked: issues.call({ do: "mode", pullRequests: false }) }
            DockChip { label: "pull requests"; chosen: !!issues.v.pullRequests; onClicked: issues.call({ do: "mode", pullRequests: true }) }
            DockChip { label: "⟳"; onClicked: issues.call({ do: "refresh" }) }
            DockChip {
                visible: !issues.v.pullRequests
                label: issues.composing ? "cancel" : "+ new"
                onClicked: issues.composing = !issues.composing
            }
        }
        Flow {
            visible: !issues.detail
            width: parent.width
            spacing: 4
            Repeater {
                model: issues.v.filters || []
                delegate: DockChip {
                    required property var modelData
                    required property int index
                    label: modelData
                    chosen: issues.v.filter === index
                    onClicked: issues.call({ do: "filter", filter: index })
                }
            }
        }
        // A folder of several repositories: all, or one.
        Flow {
            visible: !issues.detail && (issues.v.scopes || []).length > 0
            width: parent.width
            spacing: 4
            Repeater {
                model: issues.v.scopes || []
                delegate: DockChip {
                    required property var modelData
                    label: modelData.name
                    chosen: issues.v.scope === modelData.slug
                    onClicked: issues.call({ do: "scope", repo: modelData.slug })
                }
            }
        }
        // Remotes beyond origin, as the TUI's tab key walks them.
        Flow {
            visible: !issues.detail && (issues.v.remotes || []).length > 1
            width: parent.width
            spacing: 4
            Repeater {
                model: issues.v.remotes || []
                delegate: DockChip {
                    required property var modelData
                    label: modelData
                    chosen: issues.v.remote === modelData
                    onClicked: issues.call({ do: "remote", remote: modelData })
                }
            }
        }
        DockField {
            id: search
            visible: !issues.detail
            width: parent.width
            hint: "search: label:bug author:ana crash"
            onTextChanged: searchLater.restart()
            onAccepted: { searchLater.stop(); issues.call({ do: "search", query: search.text }) }
        }
        // A new issue, in the repository listed (a folder's first when all
        // are listed).
        Column {
            visible: issues.composing && !issues.detail
            width: parent.width
            spacing: 6
            DockField { id: newTitle; width: parent.width; hint: "title" }
            DockField { id: newBody; width: parent.width; multiline: true; hint: "what it is (markdown)" }
            DockChip {
                label: "file it"
                primary: true
                enabled: newTitle.text.trim() !== ""
                onClicked: {
                    issues.call({ do: "create", repo: issues.v.scope || "", title: newTitle.text, body: newBody.text })
                    newTitle.text = ""; newBody.text = ""
                    issues.composing = false
                }
            }
        }
    }

    // The list.
    ListView {
        visible: !issues.detail
        anchors.top: head.bottom
        anchors.topMargin: 8
        anchors.bottom: message.top
        width: parent.width
        clip: true
        spacing: 2
        model: issues.v.pullRequests ? (issues.v.prs || []) : (issues.v.issues || [])
        delegate: Rectangle {
            id: row
            required property var modelData
            width: ListView.view.width
            height: rowCol.height + 10
            radius: 6
            color: rowArea.containsMouse ? theme.surface0 : "transparent"
            Column {
                id: rowCol
                x: 6
                y: 5
                width: parent.width - 12
                spacing: 2
                Text {
                    width: parent.width
                    text: "<font color='" + (row.modelData.state === "open" ? theme.green : theme.overlay0) + "'>#" + row.modelData.number + "</font> " + row.modelData.title
                    textFormat: Text.StyledText
                    color: theme.text
                    font.pixelSize: 13
                    wrapMode: Text.Wrap
                }
                Text {
                    width: parent.width
                    text: (issues.v.pullRequests
                           ? [row.modelData.draft ? "draft" : row.modelData.state, row.modelData.head + " → " + row.modelData.base,
                              issues.checks(row.modelData), (row.modelData.review || "").toLowerCase().replace(/_/g, " "), row.modelData.author]
                           : [row.modelData.state, row.modelData.author, row.modelData.comments ? "💬 " + row.modelData.comments : "",
                              (row.modelData.labels || []).join(", ")]).concat([row.modelData.age, row.modelData.repo && row.modelData.repo !== issues.v.repo ? row.modelData.repo : ""])
                          .filter(Boolean).join(" · ")
                    color: theme.subtext
                    font.pixelSize: 11
                    wrapMode: Text.Wrap
                }
            }
            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: issues.call({ do: issues.v.pullRequests ? "openpr" : "open", repo: row.modelData.repo, number: row.modelData.number })
            }
        }
        Text {
            visible: parent.count === 0 && !issues.v.busy
            width: parent.width
            text: issues.v.pullRequests ? "No pull requests here." : "No issues here."
            color: theme.overlay0
            font.pixelSize: 12
            wrapMode: Text.Wrap
        }
    }

    // One issue or pull request, open.
    Flickable {
        visible: issues.detail
        anchors.top: head.bottom
        anchors.topMargin: 4
        anchors.bottom: message.top
        width: parent.width
        clip: true
        contentHeight: detailCol.height
        Column {
            id: detailCol
            width: parent.width
            spacing: 8
            readonly property var d: issues.v.pr || issues.v.issue || ({})
            readonly property bool isPR: !!issues.v.pr
            DockChip { label: "← back"; onClicked: issues.call({ do: "back" }) }
            Text {
                width: parent.width
                text: "#" + detailCol.d.number + " " + (detailCol.d.title || "")
                color: theme.text
                font.pixelSize: 15
                font.bold: true
                wrapMode: Text.Wrap
            }
            Text {
                width: parent.width
                text: (detailCol.isPR
                       ? [detailCol.d.draft ? "draft" : detailCol.d.state, "by " + detailCol.d.author, detailCol.d.head + " → " + detailCol.d.base,
                          "+" + detailCol.d.additions + " −" + detailCol.d.deletions + " in " + detailCol.d.files + " files",
                          (detailCol.d.review || "").toLowerCase().replace(/_/g, " "), (detailCol.d.mergeable || "").toLowerCase()]
                       : [detailCol.d.state, detailCol.d.author ? "by " + detailCol.d.author : "", detailCol.d.created,
                          (detailCol.d.labels || []).join(", "), (detailCol.d.assignees || []).length ? "→ " + detailCol.d.assignees.join(", ") : ""])
                      .filter(Boolean).join(" · ")
                color: theme.subtext
                font.pixelSize: 11
                wrapMode: Text.Wrap
            }
            // What can be done to it.
            Flow {
                width: parent.width
                spacing: 4
                DockChip {
                    visible: !detailCol.isPR
                    label: "✦ work on it (" + (issues.v.agent || "claude") + ")"
                    primary: true
                    onClicked: issues.call({ do: "work" })
                }
                DockChip { label: issues.commenting ? "cancel" : "comment"; onClicked: issues.commenting = !issues.commenting }
                DockChip {
                    visible: !detailCol.isPR && detailCol.d.state === "open"
                    label: "close…"
                    chosen: issues.asking === "close"
                    onClicked: issues.asking = issues.asking === "close" ? "" : "close"
                }
                DockChip {
                    visible: !detailCol.isPR && detailCol.d.state === "closed"
                    label: "reopen"
                    onClicked: issues.call({ do: "state", state: "open" })
                }
                DockChip {
                    visible: detailCol.isPR && detailCol.d.state === "open"
                    label: "merge…"
                    chosen: issues.asking === "merge"
                    onClicked: issues.asking = issues.asking === "merge" ? "" : "merge"
                }
                DockChip {
                    visible: detailCol.isPR && detailCol.d.state === "open" && !!detailCol.d.draft
                    label: "ready for review"
                    onClicked: issues.call({ do: "pr", action: "ready" })
                }
                DockChip {
                    visible: detailCol.isPR && (detailCol.d.state === "open" || detailCol.d.state === "closed")
                    label: detailCol.d.state === "open" ? "close…" : "reopen"
                    chosen: issues.asking === "closepr"
                    onClicked: {
                        if (detailCol.d.state === "open") issues.asking = issues.asking === "closepr" ? "" : "closepr"
                        else issues.call({ do: "pr", action: "reopen" })
                    }
                }
                DockChip { visible: detailCol.isPR; label: "go to its worktree"; onClicked: issues.call({ do: "worktree" }) }
                DockChip { label: "in browser ↗"; onClicked: Qt.openUrlExternally(detailCol.d.url) }
            }
            // The question a button asked, answered here, as the TUI asks
            // before closing or merging.
            Flow {
                visible: issues.asking !== ""
                width: parent.width
                spacing: 4
                Text {
                    text: issues.asking === "merge" ? "merge into " + detailCol.d.base + " by" : "close #" + detailCol.d.number + "?"
                    color: theme.text
                    font.pixelSize: 12
                    height: 24
                    verticalAlignment: Text.AlignVCenter
                }
                DockChip {
                    visible: issues.asking === "close"
                    label: "as completed"; danger: true; primary: true
                    onClicked: { issues.asking = ""; issues.call({ do: "state", state: "closed", reason: "completed" }) }
                }
                DockChip {
                    visible: issues.asking === "close"
                    label: "as not planned"; danger: true; primary: true
                    onClicked: { issues.asking = ""; issues.call({ do: "state", state: "closed", reason: "not planned" }) }
                }
                DockChip {
                    visible: issues.asking === "closepr"
                    label: "close it"; danger: true; primary: true
                    onClicked: { issues.asking = ""; issues.call({ do: "pr", action: "close" }) }
                }
                Repeater {
                    model: issues.asking === "merge" ? ["squash", "merge", "rebase"] : []
                    delegate: DockChip {
                        required property var modelData
                        label: modelData
                        primary: true
                        onClicked: { issues.asking = ""; issues.call({ do: "pr", action: "merge", method: modelData }) }
                    }
                }
                DockChip { label: "cancel"; onClicked: issues.asking = "" }
            }
            Column {
                visible: issues.commenting
                width: parent.width
                spacing: 6
                DockField { id: commentBox; width: parent.width; multiline: true; hint: "a comment (markdown); ctrl+enter sends" ; onAccepted: sendComment.clicked() }
                DockChip {
                    id: sendComment
                    label: "send"
                    primary: true
                    enabled: commentBox.text.trim() !== ""
                    onClicked: {
                        issues.call({ do: "comment", body: commentBox.text })
                        commentBox.text = ""
                        issues.commenting = false
                    }
                }
            }
            // A pull request's checks.
            Column {
                visible: detailCol.isPR && (detailCol.d.checks || []).length > 0
                width: parent.width
                spacing: 2
                Repeater {
                    model: detailCol.isPR ? (detailCol.d.checks || []) : []
                    delegate: Text {
                        required property var modelData
                        text: ({ pass: "✓ ", fail: "✗ ", skipped: "– " }[modelData.state] || "● ") + modelData.name
                        color: ({ pass: theme.green, fail: theme.red, skipped: theme.overlay0 }[modelData.state]) || theme.yellow
                        font.pixelSize: 12
                    }
                }
            }
            Rectangle { width: parent.width; height: 1; color: theme.surface0 }
            DockMarkdown { source: detailCol.d.body || (issues.v.reading ? "reading…" : "*No description.*") }
            // The pull requests an issue has: GitHub's links, and the
            // project's branches made for it.
            Column {
                visible: !detailCol.isPR && (detailCol.d.linked || []).length > 0
                width: parent.width
                spacing: 2
                Text { text: "PULL REQUESTS"; color: theme.overlay0; font.pixelSize: 11; font.bold: true; font.letterSpacing: 1 }
                Repeater {
                    model: detailCol.isPR ? [] : (detailCol.d.linked || [])
                    delegate: Text {
                        required property var modelData
                        width: detailCol.width
                        text: "#" + modelData.number + " " + modelData.state + " · " + modelData.title
                        color: theme.accent
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: issues.call({ do: "openpr", repo: modelData.repo, number: modelData.number }) }
                    }
                }
            }
            Repeater {
                model: detailCol.d.thread || []
                delegate: Column {
                    required property var modelData
                    width: detailCol.width
                    spacing: 2
                    Rectangle { width: parent.width; height: 1; color: theme.surface0 }
                    Text { text: modelData.author + " · " + modelData.age; color: theme.peach; font.pixelSize: 11 }
                    DockMarkdown { source: modelData.body }
                }
            }
        }
    }

    Text {
        id: message
        anchors.bottom: parent.bottom
        anchors.bottomMargin: (st.dock && st.dock.error) ? 30 : 0
        width: parent.width
        height: text === "" ? 0 : implicitHeight + 6
        text: issues.v.message || ""
        color: theme.subtext
        font.pixelSize: 11
        wrapMode: Text.Wrap
    }
}
