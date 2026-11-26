// TendBridge is QML's one door into Go (docs/DESKTOP.md, "Go and QML").
//
// QML reads the window's state from it — the sidebar, the tabs, where the
// panes go — as JSON Go sets when something changes, and asks for every
// action through call(method, args), which Go dispatches in one switch.
// One door, so every action the window can take is listed in one place.
// The cell size is the Terminal item's, so QML can turn the server's layout
// (in cells) into pixels the items will fill exactly.
#ifndef TEND_QTTERM_BRIDGE_H
#define TEND_QTTERM_BRIDGE_H

#include <QObject>
#include <QString>
#include <QVariantMap>

class TendBridge : public QObject {
	Q_OBJECT
	Q_PROPERTY(QString state READ state NOTIFY stateChanged)
	Q_PROPERTY(QString status READ status NOTIFY statusChanged)
	Q_PROPERTY(qreal cellWidth READ cellWidth CONSTANT)
	Q_PROPERTY(qreal cellHeight READ cellHeight CONSTANT)

public:
	explicit TendBridge(QObject *parent = nullptr);
	QString state() const { return m_state; }
	QString status() const { return m_status; }
	qreal cellWidth() const;
	qreal cellHeight() const;
	void setState(const QString &state);
	void setStatus(const QString &status);

	Q_INVOKABLE void call(const QString &method, const QVariantMap &args = QVariantMap());

signals:
	void stateChanged();
	void statusChanged();

private:
	QString m_state = QStringLiteral("{}");
	QString m_status;
};

#endif
