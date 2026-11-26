// The spending screen (issue #71): what the agents cost, drawn with
// rectangles — no chart library, no browser. "Now" is the agents open in
// the session, from the snapshot; today and this week are the activity
// record, totalled as `tend activity -by agent|company` totals it. Made
// only while it is open.
import QtQuick
import Tend 1.0

Rectangle {
    id: screen
    color: Qt.rgba(0, 0, 0, 0.5)
    readonly property var s: st.spending || ({ period: "today", total: {}, agents: [], companies: [], live: [], days: [] })

    function money(v, complete) { return "$" + (v || 0).toFixed(2) + (complete === false ? "+" : "") }
    function tokens(n) {
        n = n || 0
        if (n >= 1e6) return (n / 1e6).toFixed(1) + "M"
        if (n >= 1e3) return Math.round(n / 1e3) + "k"
        return "" + n
    }
    function maxCost(list) {
        var m = 0
        for (var i = 0; i < list.length; i++) m = Math.max(m, list[i].cost || 0)
        return m
    }

    MouseArea { anchors.fill: parent; onPressed: Bridge.call("panel", { name: "" }) }
    Keys.onEscapePressed: Bridge.call("panel", { name: "" })
    Component.onCompleted: forceActiveFocus()

    Rectangle {
        id: card
        anchors.centerIn: parent
        width: Math.min(parent.width - 80, 760)
        height: Math.min(parent.height - 60, body.height + 90)
        radius: 10
        color: theme.mantle
        border.color: theme.surface1
        MouseArea { anchors.fill: parent }

        Text {
            x: 20
            y: 16
            text: "Spending"
            color: theme.text
            font.pixelSize: 18
            font.bold: true
        }
        // The period.
        Row {
            x: 130
            y: 15
            spacing: 4
            Repeater {
                model: [{ id: "today", label: "today" }, { id: "week", label: "this week" }]
                delegate: Rectangle {
                    required property var modelData
                    readonly property bool chosen: screen.s.period === modelData.id
                    width: periodText.implicitWidth + 18
                    height: 24
                    radius: 6
                    color: chosen ? theme.accent : theme.surface0
                    Text { id: periodText; anchors.centerIn: parent; text: modelData.label; color: chosen ? theme.base : theme.text; font.pixelSize: 12 }
                    MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: Bridge.call("spending", { name: modelData.id }) }
                }
            }
        }
        Text {
            anchors.right: parent.right
            anchors.rightMargin: 16
            y: 18
            text: "×"
            color: closeArea.containsMouse ? theme.red : theme.overlay0
            font.pixelSize: 18
            MouseArea { id: closeArea; anchors.fill: parent; anchors.margins: -6; hoverEnabled: true; onClicked: Bridge.call("panel", { name: "" }) }
        }

        Flickable {
            x: 20
            y: 56
            width: parent.width - 40
            height: parent.height - 70
            contentHeight: body.height
            clip: true
            boundsBehavior: Flickable.StopAtBounds

            Column {
                id: body
                width: parent.width
                spacing: 18

                // The period's total, and what it is.
                Row {
                    spacing: 24
                    Column {
                        Text {
                            text: screen.money(screen.s.total.cost, screen.s.total.complete)
                            color: theme.text
                            font.pixelSize: 28
                            font.bold: true
                        }
                        Text {
                            text: (screen.s.period === "week" ? "since " + (screen.s.since || "Monday") : "today")
                                  + " · " + (screen.s.total.runs || 0) + ((screen.s.total.runs === 1) ? " run · " : " runs · ") + screen.tokens(screen.s.total.tokens) + " tokens"
                            color: theme.subtext
                            font.pixelSize: 12
                        }
                    }
                    Column {
                        Text {
                            text: screen.money(screen.s.liveCost, true)
                            color: theme.accent
                            font.pixelSize: 28
                            font.bold: true
                        }
                        Text {
                            text: "in the " + screen.s.live.length + " agents open now"
                            color: theme.subtext
                            font.pixelSize: 12
                        }
                    }
                }
                Text {
                    visible: !!screen.s.error || !!screen.s.loading
                    text: screen.s.error || "reading the record…"
                    color: screen.s.error ? theme.red : theme.overlay0
                    font.pixelSize: 12
                }

                // The week, a column a day.
                Column {
                    visible: (screen.s.days || []).length > 0
                    width: parent.width
                    spacing: 6
                    Heading { text: "BY DAY" }
                    Row {
                        id: days
                        width: parent.width
                        height: 130
                        spacing: 8
                        readonly property real peak: screen.maxCost(screen.s.days || [])
                        Repeater {
                            model: screen.s.days || []
                            delegate: Item {
                                required property var modelData
                                width: (days.width - 6 * days.spacing) / 7
                                height: days.height
                                Text {
                                    anchors.horizontalCenter: parent.horizontalCenter
                                    anchors.bottom: col.top
                                    anchors.bottomMargin: 2
                                    text: modelData.cost > 0 ? screen.money(modelData.cost, modelData.complete) : ""
                                    color: theme.subtext
                                    font.pixelSize: 10
                                }
                                Rectangle {
                                    id: col
                                    anchors.bottom: dayName.top
                                    anchors.bottomMargin: 4
                                    width: parent.width
                                    height: days.peak > 0 ? Math.max(modelData.cost > 0 ? 2 : 0, (days.height - 40) * modelData.cost / days.peak) : 0
                                    radius: 3
                                    color: theme.accent
                                }
                                Rectangle {
                                    anchors.bottom: dayName.top
                                    anchors.bottomMargin: 3
                                    width: parent.width
                                    height: 1
                                    color: theme.surface1
                                }
                                Text {
                                    id: dayName
                                    anchors.bottom: parent.bottom
                                    anchors.horizontalCenter: parent.horizontalCenter
                                    text: modelData.key
                                    color: theme.overlay0
                                    font.pixelSize: 11
                                }
                            }
                        }
                    }
                }

                Bars { title: "BY AGENT"; bars: screen.s.agents }
                Bars { title: "BY COMPANY"; bars: screen.s.companies }

                // The agents open now: each conversation's context against
                // its window, and its cost.
                Column {
                    width: parent.width
                    spacing: 6
                    Heading { text: "OPEN NOW" }
                    Text {
                        visible: screen.s.live.length === 0
                        text: "No agent is open in this session."
                        color: theme.overlay0
                        font.pixelSize: 12
                    }
                    Repeater {
                        model: screen.s.live
                        delegate: Item {
                            required property var modelData
                            width: body.width
                            height: 34
                            Text {
                                width: 200
                                text: modelData.agent + " · " + modelData.where
                                textFormat: Text.PlainText
                                color: theme.text
                                font.pixelSize: 12
                                elide: Text.ElideRight
                            }
                            Text {
                                y: 16
                                width: 200
                                text: (modelData.companies ? modelData.companies + " · " : "") + (modelData.model || "")
                                textFormat: Text.PlainText
                                color: theme.overlay0
                                font.pixelSize: 10
                                elide: Text.ElideRight
                            }
                            // Context, as a gauge out of the window.
                            Rectangle {
                                x: 210
                                y: 6
                                width: parent.width - 210 - 150
                                height: 10
                                radius: 5
                                color: theme.surface0
                                Rectangle {
                                    width: modelData.context >= 0 ? parent.width * Math.min(100, modelData.context) / 100 : 0
                                    height: parent.height
                                    radius: 5
                                    color: modelData.context >= 80 ? theme.red : modelData.context >= 50 ? theme.yellow : theme.green
                                }
                            }
                            Text {
                                x: 210
                                y: 18
                                text: modelData.context >= 0
                                      ? "context " + modelData.context + "% · " + screen.tokens(modelData.contextTokens) + " of " + screen.tokens(modelData.window)
                                      : "context " + screen.tokens(modelData.contextTokens)
                                color: theme.subtext
                                font.pixelSize: 10
                            }
                            Text {
                                anchors.right: parent.right
                                y: 2
                                text: screen.money(modelData.cost, modelData.complete)
                                color: theme.text
                                font.pixelSize: 13
                                font.bold: true
                            }
                            Text {
                                anchors.right: parent.right
                                y: 18
                                text: screen.tokens(modelData.tokens) + " tokens"
                                color: theme.overlay0
                                font.pixelSize: 10
                            }
                        }
                    }
                }

                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "Costs are estimates from the models' public API prices ([activity.prices] in the settings replaces them); "
                          + "a + is a floor, for a model without a price. A run is counted in each company its space is in. "
                          + "The same numbers: tend activity -by agent|company -since today|week."
                    color: theme.overlay0
                    font.pixelSize: 11
                }
            }
        }
    }

    component Heading: Text {
        color: theme.overlay0
        font.pixelSize: 11
        font.bold: true
        font.letterSpacing: 1
    }

    // A horizontal bar per key, its cost against the largest.
    component Bars: Column {
        id: chart
        property string title: ""
        property var bars: []
        readonly property real peak: screen.maxCost(chart.bars)
        width: body.width
        spacing: 6
        Heading { text: chart.title }
        Text {
            visible: chart.bars.length === 0
            text: "Nothing recorded for this period."
            color: theme.overlay0
            font.pixelSize: 12
        }
        Repeater {
            model: chart.bars
            delegate: Item {
                required property var modelData
                width: chart.width
                height: 20
                Text {
                    width: 140
                    anchors.verticalCenter: parent.verticalCenter
                    text: modelData.key
                    textFormat: Text.PlainText
                    color: theme.text
                    font.pixelSize: 12
                    elide: Text.ElideRight
                }
                Rectangle {
                    x: 150
                    anchors.verticalCenter: parent.verticalCenter
                    width: chart.peak > 0 ? Math.max(2, (parent.width - 150 - 170) * modelData.cost / chart.peak) : 2
                    height: 12
                    radius: 3
                    color: theme.accent
                }
                Text {
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    text: screen.money(modelData.cost, modelData.complete) + "  ·  " + screen.tokens(modelData.tokens) + " tok  ·  "
                          + modelData.runs + (modelData.runs === 1 ? " run" : " runs")
                    color: theme.subtext
                    font.pixelSize: 11
                }
            }
        }
    }
}
