#include "terminalitem.h"

#include <QFontDatabase>
#include <QFontMetricsF>
#include <QKeyEvent>
#include <QList>
#include <QPainter>
#include <QQuickWindow>
#include <QImage>
#include <QtQml/qqml.h>
#include <cmath>
#include <cstring>

#include "_cgo_export.h"

// Every live item, so a frame for a pane reaches whichever items show it.
// Main thread only, like everything else that touches the scene.
static QList<TerminalItem *> g_items;

// Attribute bits, vt.Attr's (core.AttrBold and the rest).
enum : uint16_t {
	AttrBold = 1 << 0,
	AttrDim = 1 << 1,
	AttrItalic = 1 << 2,
	AttrUnderline = 1 << 7,
	AttrStrike = 1 << 6,
};

static QColor rgb(uint32_t c) { return QColor((c >> 16) & 0xff, (c >> 8) & 0xff, c & 0xff); }

TerminalItem::TerminalItem(QQuickItem *parent) : QQuickPaintedItem(parent) {
	setFlag(ItemAcceptsInputMethod, false);
	setFlag(ItemIsFocusScope, true);
	setAcceptedMouseButtons(Qt::AllButtons);
	setActiveFocusOnTab(false);
	setOpaquePainting(true);
	m_font = QFontDatabase::systemFont(QFontDatabase::FixedFont);
	m_font.setPointSizeF(11);
	m_font.setStyleHint(QFont::Monospace);
	measure();
	g_items.append(this);
}

TerminalItem::~TerminalItem() { g_items.removeAll(this); }

void TerminalItem::setPane(qint64 pane) {
	if (pane == m_pane)
		return;
	m_pane = pane;
	m_cells.clear();
	m_cols = m_rows = 0;
	// A new pane has not been told this item's size yet.
	m_reportedCols = m_reportedRows = 0;
	emit paneChanged();
	reportSize();
	update();
}

void TerminalItem::setFontFamily(const QString &family) {
	if (family.isEmpty() || family == m_font.family())
		return;
	m_font.setFamily(family);
	measure();
	emit fontChanged();
	reportSize();
	update();
}

void TerminalItem::setFontSize(qreal size) {
	if (size <= 0 || size == m_font.pointSizeF())
		return;
	m_font.setPointSizeF(size);
	measure();
	emit fontChanged();
	reportSize();
	update();
}

// measure takes the cell size from the font: the advance of a character for
// the width, and the line spacing for the height, both rounded up to whole
// pixels so the grid lines up and no row bleeds into the next.
void TerminalItem::measure() {
	QFontMetricsF fm(m_font);
	m_cellW = std::ceil(fm.horizontalAdvance(QLatin1Char('M')));
	m_cellH = std::ceil(fm.lineSpacing());
	m_ascent = std::round(fm.ascent());
}

void TerminalItem::geometryChange(const QRectF &now, const QRectF &before) {
	QQuickPaintedItem::geometryChange(now, before);
	reportSize();
}

// reportSize tells Go how many cells fit, when that changes. Go resizes the
// pane's terminal on the server to it.
void TerminalItem::reportSize() {
	if (m_pane == 0 || width() <= 0 || height() <= 0)
		return;
	int cols = std::max(1, int(width() / m_cellW));
	int rows = std::max(1, int(height() / m_cellH));
	if (cols == m_reportedCols && rows == m_reportedRows)
		return;
	m_reportedCols = cols;
	m_reportedRows = rows;
	goTermResize(m_pane, cols, rows);
}

void TerminalItem::setFrame(const TendCell *cells, int cols, int rows, int cx, int cy, bool cursor, uint32_t bg) {
	m_cells.assign(cells, cells + size_t(cols) * size_t(rows));
	m_cols = cols;
	m_rows = rows;
	m_cx = cx;
	m_cy = cy;
	m_cursor = cursor;
	m_bg = bg;
	update();
}

void TerminalItem::paint(QPainter *p) {
	p->fillRect(QRectF(0, 0, width(), height()), rgb(m_bg));
	if (m_cells.empty())
		return;

	// Backgrounds first, a run of one colour at a time, so the text drawn
	// next is never cut by the fill of the cell after it.
	for (int y = 0; y < m_rows; y++) {
		const TendCell *row = &m_cells[size_t(y) * m_cols];
		int x = 0;
		while (x < m_cols) {
			uint32_t bg = row[x].bg;
			int end = x + 1;
			while (end < m_cols && row[end].bg == bg)
				end++;
			if (bg != m_bg)
				p->fillRect(QRectF(x * m_cellW, y * m_cellH, (end - x) * m_cellW, m_cellH), rgb(bg));
			x = end;
		}
	}

	// Then the text. A run of plain ASCII in one style is drawn as one
	// string, which a monospaced font lays out on the grid by itself; any
	// other character is drawn in its own cell, so a glyph from a fallback
	// font of another width cannot push the rest of the line off the grid.
	QFont font = m_font;
	QString run;
	for (int y = 0; y < m_rows; y++) {
		const TendCell *row = &m_cells[size_t(y) * m_cols];
		qreal baseline = y * m_cellH + m_ascent;
		int x = 0;
		while (x < m_cols) {
			const TendCell &c = row[x];
			if (c.width == 0 || c.ch == ' ') {
				x++;
				continue;
			}
			uint32_t fg = c.fg;
			uint16_t attrs = c.attrs;
			font.setBold(attrs & AttrBold);
			font.setItalic(attrs & AttrItalic);
			p->setFont(font);
			QColor colour = rgb(fg);
			if (attrs & AttrDim)
				colour.setAlphaF(0.6);
			p->setPen(colour);

			int start = x;
			run.clear();
			if (c.ch < 0x80) {
				while (x < m_cols && row[x].ch < 0x80 && row[x].width == 1 && row[x].fg == fg &&
				       row[x].attrs == attrs) {
					run.append(QChar(char16_t(row[x].ch)));
					x++;
				}
			} else {
				run = QString::fromUcs4(reinterpret_cast<const char32_t *>(&c.ch), 1);
				x += c.width > 1 ? c.width : 1;
			}
			p->drawText(QPointF(start * m_cellW, baseline), run);
			qreal w = (x - start) * m_cellW;
			if (attrs & AttrUnderline)
				p->fillRect(QRectF(start * m_cellW, baseline + 2, w, 1), colour);
			if (attrs & AttrStrike)
				p->fillRect(QRectF(start * m_cellW, y * m_cellH + m_cellH / 2, w, 1), colour);
		}
	}

	if (m_cursor && m_cx >= 0 && m_cx < m_cols && m_cy >= 0 && m_cy < m_rows) {
		const TendCell &c = m_cells[size_t(m_cy) * m_cols + m_cx];
		QRectF r(m_cx * m_cellW, m_cy * m_cellH, m_cellW * (c.width > 1 ? 2 : 1), m_cellH);
		if (hasActiveFocus()) {
			// A block in the cell's foreground, with its character on it in
			// the background colour, as a terminal's focused cursor is.
			p->fillRect(r, rgb(c.fg));
			if (c.ch != ' ' && c.ch != 0) {
				font.setBold(c.attrs & AttrBold);
				font.setItalic(c.attrs & AttrItalic);
				p->setFont(font);
				p->setPen(rgb(c.bg));
				p->drawText(QPointF(r.x(), m_cy * m_cellH + m_ascent),
				            QString::fromUcs4(reinterpret_cast<const char32_t *>(&c.ch), 1));
			}
		} else {
			p->setPen(rgb(c.fg));
			p->setBrush(Qt::NoBrush);
			p->drawRect(r.adjusted(0.5, 0.5, -0.5, -0.5));
		}
	}
}

void TerminalItem::keyPressEvent(QKeyEvent *event) {
	if (m_pane == 0) {
		event->ignore();
		return;
	}
	QByteArray text = event->text().toUtf8();
	goTermKey(m_pane, event->key(), int(event->modifiers()), text.data(), int(text.size()));
	event->accept();
}

void TerminalItem::mousePressEvent(QMouseEvent *event) {
	forceActiveFocus(Qt::MouseFocusReason);
	event->accept();
}

// The cursor is drawn differently with and without the focus.
void TerminalItem::focusInEvent(QFocusEvent *event) {
	QQuickPaintedItem::focusInEvent(event);
	update();
}

void TerminalItem::focusOutEvent(QFocusEvent *event) {
	QQuickPaintedItem::focusOutEvent(event);
	update();
}

extern "C" void tend_term_register(void) { qmlRegisterType<TerminalItem>("Tend", 1, 0, "Terminal"); }

extern "C" void tend_term_set(int64_t pane, const TendCell *cells, int cols, int rows, int cursor_x,
                              int cursor_y, int cursor_visible, uint32_t bg) {
	for (TerminalItem *item : g_items) {
		if (item->pane() == pane)
			item->setFrame(cells, cols, rows, cursor_x, cursor_y, cursor_visible != 0, bg);
	}
}

extern "C" int tend_term_grab(const char *path) {
	if (g_items.isEmpty() || !g_items.first()->window())
		return 1;
	QImage shot = g_items.first()->window()->grabWindow();
	return shot.save(QString::fromUtf8(path), "PNG") ? 0 : 2;
}

// moc's output for TerminalItem, generated by `go generate` (qtterm.go). It is
// named .moc, not .cpp, so cgo does not compile it a second time on its own.
#include "terminalitem.moc"
