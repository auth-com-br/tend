#include "terminalitem.h"

#include <QFontDatabase>
#include <QFontMetricsF>
#include <QClipboard>
#include <QGuiApplication>
#include <QKeyEvent>
#include <QList>
#include <QPainter>
#include <QQuickWindow>
#include <QImage>
#include <QtQml/qqml.h>
#include <algorithm>
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
	setAcceptHoverEvents(true);
	setActiveFocusOnTab(false);
	setOpaquePainting(true);
	m_font = defaultFont();
	measure();
	g_items.append(this);
}

TerminalItem::~TerminalItem() { g_items.removeAll(this); }

QFont TerminalItem::defaultFont() {
	QFont font = QFontDatabase::systemFont(QFontDatabase::FixedFont);
	font.setPointSizeF(13);
	font.setStyleHint(QFont::Monospace);
	return font;
}

// The same rounding as measure(), so a pane laid out in these cells is
// filled by its item to the cell.
QSizeF TerminalItem::defaultCellSize() {
	QFontMetricsF fm(defaultFont());
	return QSizeF(std::ceil(fm.horizontalAdvance(QLatin1Char('M'))), std::ceil(fm.lineSpacing()));
}

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
	m_variantsReady = false;
}

// variant is the font in one style, spaced so that each character advances
// exactly one cell. The cell is the font's advance rounded up to a whole
// pixel, and a run of text is drawn as one string: without the spacing, a
// word of eight letters ended three pixels short of its cells and every
// gap between words grew by that ("Allow  external  CLAUDE.md" in Claude
// Code's prompts). Bold is measured on its own: a font with no bold face is
// made bold by Qt, wider.
const QFont &TerminalItem::variant(bool bold, bool italic) {
	if (!m_variantsReady) {
		for (int i = 0; i < 4; i++) {
			QFont f = m_font;
			f.setBold(i & 1);
			f.setItalic(i & 2);
			f.setKerning(false);
			qreal advance = QFontMetricsF(f).horizontalAdvance(QLatin1Char('M'));
			f.setLetterSpacing(QFont::AbsoluteSpacing, m_cellW - advance);
			m_variants[i] = f;
		}
		m_variantsReady = true;
	}
	return m_variants[(bold ? 1 : 0) | (italic ? 2 : 0)];
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
			p->setFont(variant(attrs & AttrBold, attrs & AttrItalic));
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
				p->setFont(variant(c.attrs & AttrBold, c.attrs & AttrItalic));
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
	// Ctrl+Shift+V pastes, as in a desktop terminal: the clipboard goes to
	// the pane as a paste (bracketed when the program asked), not as keys.
	if (event->key() == Qt::Key_V && (event->modifiers() & Qt::ControlModifier) &&
	    (event->modifiers() & Qt::ShiftModifier)) {
		QByteArray paste = QGuiApplication::clipboard()->text().toUtf8();
		if (!paste.isEmpty())
			goTermPaste(m_pane, paste.data(), int(paste.size()));
		event->accept();
		return;
	}
	QByteArray text = event->text().toUtf8();
	goTermKey(m_pane, event->key(), int(event->modifiers()), text.data(), int(text.size()));
	event->accept();
}

// The mouse goes to Go as cells and xterm's button numbers (0 left, 1
// middle, 2 right) with what Go decides from: whether the pane's program
// asked for the mouse, and so gets it, or tend selects and scrolls.
// Kinds are ui.MouseKind's: 1 press, 2 release, 3 drag, 4 move, 5 wheel up,
// 6 wheel down. Modifiers are ui.MouseMod's: 1 shift, 2 alt, 4 ctrl.
static int buttonCode(Qt::MouseButton b) {
	switch (b) {
	case Qt::LeftButton:
		return 0;
	case Qt::MiddleButton:
		return 1;
	case Qt::RightButton:
		return 2;
	default:
		return -1;
	}
}

void TerminalItem::reportMouse(int kind, int button, QPointF pos, Qt::KeyboardModifiers mods) {
	if (m_pane == 0)
		return;
	int x = std::clamp(int(pos.x() / m_cellW), 0, std::max(0, m_reportedCols - 1));
	int y = std::clamp(int(pos.y() / m_cellH), 0, std::max(0, m_reportedRows - 1));
	if ((kind == 3 || kind == 4) && x == m_lastX && y == m_lastY)
		return;
	m_lastX = x;
	m_lastY = y;
	int m = 0;
	if (mods & Qt::ShiftModifier)
		m |= 1;
	if (mods & Qt::AltModifier)
		m |= 2;
	if (mods & Qt::ControlModifier)
		m |= 4;
	goTermMouse(m_pane, kind, button, x, y, m);
}

void TerminalItem::mousePressEvent(QMouseEvent *event) {
	forceActiveFocus(Qt::MouseFocusReason);
	emit activated();
	int b = buttonCode(event->button());
	if (b >= 0) {
		m_button = b;
		m_lastX = m_lastY = -1;
		reportMouse(1, b, event->position(), event->modifiers());
	}
	event->accept();
}

void TerminalItem::mouseMoveEvent(QMouseEvent *event) {
	if (m_button >= 0)
		reportMouse(3, m_button, event->position(), event->modifiers());
	event->accept();
}

void TerminalItem::mouseReleaseEvent(QMouseEvent *event) {
	int b = buttonCode(event->button());
	if (b >= 0) {
		m_lastX = m_lastY = -1;
		reportMouse(2, b, event->position(), event->modifiers());
		if (b == m_button)
			m_button = -1;
	}
	event->accept();
}

void TerminalItem::hoverMoveEvent(QHoverEvent *event) {
	if (m_button < 0)
		reportMouse(4, 0, event->position(), event->modifiers());
	QQuickPaintedItem::hoverMoveEvent(event);
}

// A notch of the wheel is 120; a touchpad sends it in parts, which are
// added up so a slow swipe still scrolls.
void TerminalItem::wheelEvent(QWheelEvent *event) {
	m_wheel += event->angleDelta().y();
	while (m_wheel >= 120) {
		m_wheel -= 120;
		m_lastX = m_lastY = -1;
		reportMouse(5, 0, event->position(), event->modifiers());
	}
	while (m_wheel <= -120) {
		m_wheel += 120;
		m_lastX = m_lastY = -1;
		reportMouse(6, 0, event->position(), event->modifiers());
	}
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
