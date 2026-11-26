// The settings screen: the TUI's rows (internal/settings), each changed
// where it is seen and written to the settings file, which is read again
// here and on the server. Made only while it is open.
import QtQuick
import Tend 1.0

Rectangle {
    id: screen
    color: Qt.rgba(0, 0, 0, 0.5)
    readonly property var s: st.settings || ({ rows: [], path: "" })

    MouseArea { anchors.fill: parent; onPressed: Bridge.call("panel", { name: "" }) }
    Keys.onEscapePressed: Bridge.call("panel", { name: "" })
    Component.onCompleted: forceActiveFocus()

    Rectangle {
        anchors.centerIn: parent
        width: Math.min(parent.width - 80, 640)
        height: Math.min(parent.height - 80, list.contentHeight + 110)
        radius: 10
        color: theme.mantle
        border.color: theme.surface1
        MouseArea { anchors.fill: parent }

        Text {
            x: 20
            y: 16
            text: "Settings"
            color: theme.text
            font.pixelSize: 18
            font.bold: true
        }
        Text {
            anchors.right: parent.right
            anchors.rightMargin: 16
            y: 18
            text: "×"
            color: closeArea.containsMouse ? theme.red : theme.overlay0
            font.pixelSize: 18
            MouseArea {
                id: closeArea
                anchors.fill: parent
                anchors.margins: -6
                hoverEnabled: true
                onClicked: Bridge.call("panel", { name: "" })
            }
        }

        ListView {
            id: list
            x: 12
            y: 56
            width: parent.width - 24
            height: parent.height - 100
            clip: true
            spacing: 2
            model: screen.s.rows
            boundsBehavior: Flickable.StopAtBounds
            delegate: Rectangle {
                id: row
                required property var modelData
                required property int index
                width: list.width
                height: 40
                radius: 6
                color: rowHover.containsMouse ? theme.surface0 : "transparent"
                MouseArea { id: rowHover; anchors.fill: parent; hoverEnabled: true; acceptedButtons: Qt.NoButton }
                Text {
                    x: 10
                    anchors.verticalCenter: parent.verticalCenter
                    width: 190
                    text: row.modelData.label
                    color: theme.text
                    font.pixelSize: 14
                    elide: Text.ElideRight
                }
                // Up to four choices side by side; more (the themes) as the
                // current one with a step either way.
                Row {
                    x: 210
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 4
                    visible: row.modelData.choices.length <= 4
                    Repeater {
                        model: row.modelData.choices
                        delegate: Rectangle {
                            required property string modelData
                            required property int index
                            readonly property bool chosen: index === row.modelData.current
                            height: 28
                            width: choiceText.width + 20
                            radius: 6
                            color: chosen ? theme.accent : (choiceArea.containsMouse ? theme.surface1 : theme.surface0)
                            Text {
                                id: choiceText
                                anchors.centerIn: parent
                                text: modelData
                                color: chosen ? theme.base : theme.text
                                font.pixelSize: 13
                            }
                            MouseArea {
                                id: choiceArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                // A step from the current choice to this one.
                                onClicked: if (!chosen) Bridge.call("setting.change",
                                    { index: row.index, delta: index - Math.max(0, row.modelData.current) })
                            }
                        }
                    }
                }
                Row {
                    x: 210
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 8
                    visible: row.modelData.choices.length > 4
                    Stepper { glyph: "‹"; onClicked: Bridge.call("setting.change", { index: row.index, delta: -1 }) }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        width: 180
                        horizontalAlignment: Text.AlignHCenter
                        text: row.modelData.current >= 0 ? row.modelData.choices[row.modelData.current] : "—"
                        color: theme.text
                        font.pixelSize: 14
                    }
                    Stepper { glyph: "›"; onClicked: Bridge.call("setting.change", { index: row.index, delta: 1 }) }
                }
            }
        }

        Text {
            x: 20
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 14
            width: parent.width - 40
            text: "written to " + screen.s.path
            color: theme.overlay0
            font.pixelSize: 11
            elide: Text.ElideMiddle
        }
    }

    component Stepper: Rectangle {
        id: st
        property string glyph: ""
        signal clicked()
        width: 28
        height: 28
        radius: 6
        color: stArea.containsMouse ? theme.surface1 : theme.surface0
        Text { anchors.centerIn: parent; text: st.glyph; color: theme.text; font.pixelSize: 16 }
        MouseArea { id: stArea; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: st.clicked() }
    }
}
