#include <ghostty/vt.h>

// Small helpers for Swift: sized-struct setup, and per-cell extraction into
// one flat struct so a row costs one call instead of a dozen per cell.

typedef struct {
  uint8_t text[32];  // UTF-8 grapheme cluster
  uint8_t text_len;
  uint8_t wide;      // GhosttyCellWide
  uint8_t has_fg, has_bg;
  GhosttyColorRgb fg, bg;
  bool bold, italic, faint, inverse, invisible, strikethrough, blink;
  uint8_t underline;  // GHOSTTY_SGR_UNDERLINE_*
  bool selected;
} HiveCell;

typedef struct {
  uint16_t cols, rows;
  uint8_t dirty;  // GhosttyRenderStateDirty
  GhosttyColorRgb background, foreground, cursor_color;
  bool has_cursor_color;
  bool cursor_visible, cursor_in_viewport, cursor_blinking;
  uint16_t cursor_x, cursor_y;
  uint8_t cursor_style;  // GhosttyRenderStateCursorVisualStyle
} HiveFrameMeta;

GhosttyResult hive_terminal_new(uint16_t cols, uint16_t rows, GhosttyTerminal *out);
GhosttyResult hive_terminal_from_snapshot(const uint8_t *data, size_t len, GhosttyTerminal *out);
GhosttyResult hive_snapshot(GhosttyTerminal t, uint8_t **out, size_t *len);
GhosttyResult hive_format_plain(GhosttyTerminal t, uint8_t **out, size_t *len);
void hive_free(uint8_t *ptr, size_t len);

GhosttyResult hive_frame_meta(GhosttyRenderState rs, HiveFrameMeta *out);
// Fills up to cap cells of the iterator's current row. Returns the count.
size_t hive_row_cells(GhosttyRenderStateRowIterator it, GhosttyRenderStateRowCells cells, HiveCell *out, size_t cap);
bool hive_row_dirty(GhosttyRenderStateRowIterator it);
void hive_row_set_clean(GhosttyRenderStateRowIterator it);

// Encodes a key press for the terminal's current modes into out (cap bytes).
size_t hive_encode_key(GhosttyTerminal t, GhosttyKey key, GhosttyMods mods, const char *utf8, size_t utf8_len,
                       uint32_t unshifted, uint8_t *out, size_t cap);
// Encodes pasted text, bracketed when the program enabled mode 2004.
GhosttyResult hive_encode_paste(GhosttyTerminal t, char *data, size_t len, uint8_t *out, size_t cap, size_t *written);
bool hive_mode(GhosttyTerminal t, uint16_t number, bool ansi);
void hive_set_render_hold(GhosttyTerminal t, GhosttyTerminalRenderHoldFn fn, void *userdata);
void hive_scroll_delta(GhosttyTerminal t, intptr_t rows);
void hive_scroll_bottom(GhosttyTerminal t);
