// TerminalItem draws one pane's screen in a QML scene.
//
// It is C++ because the Go bindings (miqt) cover the QML engine but not Qt
// Quick: there is no QQuickItem to subclass from Go. So this is the smallest
// piece that has to be: it paints the cells Go gives it and reports keys and
// its size back. Every decision — what the cells are, which colour, what a
// key sends — is made in Go (desktop/internal/core).
#ifndef TEND_QTTERM_TERMINALITEM_H
#define TEND_QTTERM_TERMINALITEM_H

#include <QFont>
#include <QSizeF>
#include <QQuickPaintedItem>
#include <vector>

#include "terminal.h"

class TerminalItem : public QQuickPaintedItem {
	Q_OBJECT
	Q_PROPERTY(qint64 pane READ pane WRITE setPane NOTIFY paneChanged)
	Q_PROPERTY(QString fontFamily READ fontFamily WRITE setFontFamily NOTIFY fontChanged)
	Q_PROPERTY(qreal fontSize READ fontSize WRITE setFontSize NOTIFY fontChanged)

public:
	explicit TerminalItem(QQuickItem *parent = nullptr);
	~TerminalItem() override;

	qint64 pane() const { return m_pane; }
	void setPane(qint64 pane);
	QString fontFamily() const { return m_font.family(); }
	void setFontFamily(const QString &family);
	qreal fontSize() const { return m_font.pointSizeF(); }
	void setFontSize(qreal size);

	// defaultCellSize is the cell of the font an item starts with, which
	// the Bridge gives QML for laying panes out in pixels.
	static QSizeF defaultCellSize();
	static QFont defaultFont();

	void setFrame(const TendCell *cells, int cols, int rows, int cx, int cy, bool cursor, uint32_t bg);
	void paint(QPainter *painter) override;

signals:
	void paneChanged();
	void fontChanged();
	// activated is a press on the item: QML gives its pane the focus.
	void activated();

protected:
	void keyPressEvent(QKeyEvent *event) override;
	void geometryChange(const QRectF &now, const QRectF &before) override;
	void mousePressEvent(QMouseEvent *event) override;
	void mouseMoveEvent(QMouseEvent *event) override;
	void mouseReleaseEvent(QMouseEvent *event) override;
	void hoverMoveEvent(QHoverEvent *event) override;
	void wheelEvent(QWheelEvent *event) override;
	void focusInEvent(QFocusEvent *event) override;
	void focusOutEvent(QFocusEvent *event) override;

private:
	void measure();
	void reportSize();
	void reportMouse(int kind, int button, QPointF pos, Qt::KeyboardModifiers mods);
	const QFont &variant(bool bold, bool italic);

	qint64 m_pane = 0;
	QFont m_font;
	// m_variants are the font in its four styles, each spaced so that one
	// character takes exactly one cell (see variant).
	QFont m_variants[4];
	bool m_variantsReady = false;
	qreal m_cellW = 8, m_cellH = 16, m_ascent = 12;
	int m_cols = 0, m_rows = 0;
	int m_reportedCols = 0, m_reportedRows = 0;
	std::vector<TendCell> m_cells;
	int m_cx = 0, m_cy = 0;
	bool m_cursor = false;
	uint32_t m_bg = 0x1e1e2e;
	// The mouse, as reported to Go: the button held, the cell last
	// reported (a move within one cell says nothing new), and the wheel's
	// part-notches, which a touchpad sends in small steps.
	int m_button = -1;
	int m_lastX = -1, m_lastY = -1;
	int m_wheel = 0;
};

#endif
