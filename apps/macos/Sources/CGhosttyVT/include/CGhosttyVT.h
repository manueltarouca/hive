#include <ghostty/vt.h>

// Small helpers for Swift: sized-struct setup, and per-cell extraction into
// one flat struct so a row costs one call instead of a dozen per cell.

typedef struct {
  uint8_t text[64];  // UTF-8 grapheme cluster (family emoji run long)
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

/// The cursor's default blink: -1 (blank, Ghostty's default) and 1 blink,
/// 0 doesn't. Programs still change it with DECSCUSR or DEC mode 12.
void hive_set_default_cursor_blink(GhosttyTerminal t, int blink);
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
// Returns the bytes written; if cap was too small, returns 0 and sets
// *needed to the size required.
size_t hive_encode_key(GhosttyTerminal t, GhosttyKey key, GhosttyMods mods, GhosttyMods consumed, const char *utf8, size_t utf8_len,
                       uint32_t unshifted, uint8_t *out, size_t cap, size_t *needed);
// Whether pasting data is safe: always when the program asked for
// bracketed paste, otherwise only without newlines and control bytes.
bool hive_paste_is_safe(GhosttyTerminal t, const char *data, size_t len);
// Encodes pasted text, bracketed when the program enabled mode 2004.
GhosttyResult hive_encode_paste(GhosttyTerminal t, char *data, size_t len, uint8_t *out, size_t cap, size_t *written);
bool hive_mode(GhosttyTerminal t, uint16_t number, bool ansi);
void hive_set_render_hold(GhosttyTerminal t, GhosttyTerminalRenderHoldFn fn, void *userdata);
void hive_scroll_delta(GhosttyTerminal t, intptr_t rows);
void hive_scroll_bottom(GhosttyTerminal t);

// Sets default colors and the first 16 palette entries (the rest keep
// libghostty's 256-color defaults).
void hive_set_theme(GhosttyTerminal t, GhosttyColorRgb fg, GhosttyColorRgb bg, GhosttyColorRgb cursor,
                    const GhosttyColorRgb *palette16);

// Selection through libghostty's gesture engine (click counting, word and
// line selection, drag). type: 0 press, 1 release, 2 drag. The resulting
// selection is set on the terminal, so the render state marks it.
GhosttySelectionGesture hive_gesture_new(void);
void hive_gesture_free(GhosttySelectionGesture g, GhosttyTerminal t);
void hive_select(GhosttyTerminal t, GhosttySelectionGesture g, int type, uint16_t col, uint16_t row,
                 double x, double y, uint32_t cols, uint32_t cell_width, uint32_t screen_height, uint64_t time_ns,
                 uint64_t repeat_interval_ns, double repeat_distance);
// The selected text, or NULL. Free with hive_free.
bool hive_selection_text(GhosttyTerminal t, uint8_t **out, size_t *len);
GhosttyResult hive_hyperlink_at(GhosttyTerminal t, uint16_t col, uint16_t row, uint8_t *buf, size_t cap, size_t *len);
size_t hive_row_graphemes(GhosttyTerminal t, uint16_t row, uint32_t *out, uint8_t *counts, size_t cols, size_t per);
void hive_clear_selection(GhosttyTerminal t);

// Pointer geometry in surface pixels, for the mouse encoder.
typedef struct {
  uint32_t screen_width, screen_height, cell_width, cell_height;
} HivePointerGeometry;

// Scroll-wheel routing, as Ghostty does it. rows < 0 scrolls up.
// Returns 0 when the caller should scroll its own viewport, or 1 with the
// bytes for the program in out (none when its tracking mode ignores wheels):
// wheel reports when the program enabled mouse tracking, else arrow keys on
// the alternate screen with alternate scroll (1007) on. Shift forces 0.
int hive_wheel(GhosttyTerminal t, intptr_t rows, bool shift, float x, float y, HivePointerGeometry g,
               uint8_t *out, size_t cap, size_t *written);

// A mouse encoder kept per terminal, so motion within one cell isn't
// reported twice. Free with hive_mouse_encoder_free.
GhosttyMouseEncoder hive_mouse_encoder_new(void);
void hive_mouse_encoder_free(GhosttyMouseEncoder enc);

// A click, release or motion for a program that tracks the mouse.
// action: 0 press, 1 release, 2 motion. button: 0 none, 1 left, 2 right,
// 3 middle. any_pressed: a button is held (for motion during a drag).
// Returns -1 when the program isn't tracking the mouse (the caller selects
// locally), else the bytes written to out (0 when this event isn't reported).
intptr_t hive_mouse(GhosttyTerminal t, GhosttyMouseEncoder enc, int action, int button, GhosttyMods mods,
                    bool any_pressed, float x, float y, HivePointerGeometry g, uint8_t *out, size_t cap);

/// Whether the program in the terminal has turned on mouse reporting.
bool hive_mouse_tracking(GhosttyTerminal t);

