#include "CGhosttyVT.h"
#include <string.h>

static void configure(GhosttyTerminal t) {
  // The mirror never draws images (cut from M2) and keeps the same
  // scrollback cap as the server.
  uint64_t no_images = 0;
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_KITTY_IMAGE_STORAGE_LIMIT, &no_images);
  size_t scrollback = 16u << 20;
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_BYTES, &scrollback);
}

GhosttyResult hive_terminal_new(uint16_t cols, uint16_t rows, GhosttyTerminal *out) {
  GhosttyResult r = ghostty_terminal_new(NULL, out, cols, rows);
  if (r == GHOSTTY_SUCCESS) configure(*out);
  return r;
}

GhosttyResult hive_terminal_from_snapshot(const uint8_t *data, size_t len, GhosttyTerminal *out) {
  GhosttySnapshotDecoder d = NULL;
  GhosttyResult r = ghostty_snapshot_decoder_new_buf(NULL, &d, data, len);
  if (r != GHOSTTY_SUCCESS) return r;
  r = ghostty_snapshot_decoder_decode(d, out);
  ghostty_snapshot_decoder_free(d);
  if (r == GHOSTTY_SUCCESS) configure(*out);
  return r;
}

GhosttyResult hive_snapshot(GhosttyTerminal t, uint8_t **out, size_t *len) {
  return ghostty_snapshot_encode_alloc(t, NULL, out, len);
}

GhosttyResult hive_format_plain(GhosttyTerminal t, uint8_t **out, size_t *len) {
  GhosttyFormatterTerminalOptions o = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
  o.emit = GHOSTTY_FORMATTER_FORMAT_PLAIN;
  o.trim = true;
  o.extra = (GhosttyFormatterTerminalExtra)GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalExtra);
  o.extra.screen = (GhosttyFormatterScreenExtra)GHOSTTY_INIT_SIZED(GhosttyFormatterScreenExtra);
  GhosttyFormatter f = NULL;
  GhosttyResult r = ghostty_formatter_terminal_new(NULL, &f, t, o);
  if (r != GHOSTTY_SUCCESS) return r;
  r = ghostty_formatter_format_alloc(f, NULL, out, len);
  ghostty_formatter_free(f);
  return r;
}

void hive_free(uint8_t *ptr, size_t len) { ghostty_free(NULL, ptr, len); }

GhosttyResult hive_frame_meta(GhosttyRenderState rs, HiveFrameMeta *out) {
  memset(out, 0, sizeof(*out));
  ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_COLS, &out->cols);
  ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_ROWS, &out->rows);
  GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
  out->dirty = (uint8_t)dirty;
  GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
  GhosttyResult r = ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors);
  if (r != GHOSTTY_SUCCESS) return r;
  out->background = colors.background;
  out->foreground = colors.foreground;
  GhosttyRenderStateCursor c = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
  r = ghostty_render_state_get(rs, GHOSTTY_RENDER_STATE_DATA_CURSOR, &c);
  if (r != GHOSTTY_SUCCESS) return r;
  out->cursor_visible = c.visible;
  out->cursor_in_viewport = c.viewport_has_value;
  out->cursor_blinking = c.blinking;
  out->cursor_x = c.viewport_x;
  out->cursor_y = c.viewport_y;
  out->cursor_style = (uint8_t)c.visual_style;
  return GHOSTTY_SUCCESS;
}

bool hive_row_dirty(GhosttyRenderStateRowIterator it) {
  bool d = false;
  ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY, &d);
  return d;
}

void hive_row_set_clean(GhosttyRenderStateRowIterator it) {
  bool d = false;
  ghostty_render_state_row_set(it, GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, &d);
}

size_t hive_row_cells(GhosttyRenderStateRowIterator it, GhosttyRenderStateRowCells cells, HiveCell *out, size_t cap) {
  if (ghostty_render_state_row_get(it, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &cells) != GHOSTTY_SUCCESS) return 0;
  size_t n = 0;
  while (n < cap && ghostty_render_state_row_cells_next(cells)) {
    HiveCell *c = &out[n++];
    memset(c, 0, sizeof(*c));
    GhosttyBuffer buf = {.ptr = c->text, .cap = sizeof(c->text) - 1, .len = 0};
    if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_UTF8, &buf) == GHOSTTY_SUCCESS)
      c->text_len = (uint8_t)buf.len;
    GhosttyCell raw = 0;
    if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw) == GHOSTTY_SUCCESS) {
      GhosttyCellWide w = GHOSTTY_CELL_WIDE_NARROW;
      ghostty_cell_get(raw, GHOSTTY_CELL_DATA_WIDE, &w);
      c->wide = (uint8_t)w;
    }
    c->has_fg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &c->fg) == GHOSTTY_SUCCESS;
    c->has_bg = ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &c->bg) == GHOSTTY_SUCCESS;
    bool styled = false;
    ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_HAS_STYLING, &styled);
    if (styled) {
      GhosttyStyle s = GHOSTTY_INIT_SIZED(GhosttyStyle);
      if (ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &s) == GHOSTTY_SUCCESS) {
        c->bold = s.bold; c->italic = s.italic; c->faint = s.faint; c->inverse = s.inverse;
        c->invisible = s.invisible; c->strikethrough = s.strikethrough; c->blink = s.blink;
        c->underline = (uint8_t)s.underline;
      }
    }
    bool sel = false;
    ghostty_render_state_row_cells_get(cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_SELECTED, &sel);
    c->selected = sel;
  }
  return n;
}

size_t hive_encode_key(GhosttyTerminal t, GhosttyKey key, GhosttyMods mods, const char *utf8, size_t utf8_len,
                       uint32_t unshifted, uint8_t *out, size_t cap, size_t *needed) {
  *needed = 0;
  GhosttyKeyEncoder enc = NULL;
  if (ghostty_key_encoder_new(NULL, &enc) != GHOSTTY_SUCCESS) return 0;
  ghostty_key_encoder_setopt_from_terminal(enc, t);
  GhosttyKeyEvent ev = NULL;
  if (ghostty_key_event_new(NULL, &ev) != GHOSTTY_SUCCESS) { ghostty_key_encoder_free(enc); return 0; }
  ghostty_key_event_set_action(ev, GHOSTTY_KEY_ACTION_PRESS);
  ghostty_key_event_set_key(ev, key);
  ghostty_key_event_set_mods(ev, mods);
  if (utf8_len > 0) ghostty_key_event_set_utf8(ev, utf8, utf8_len);
  ghostty_key_event_set_unshifted_codepoint(ev, unshifted);
  size_t written = 0;
  GhosttyResult r = ghostty_key_encoder_encode(enc, ev, (char *)out, cap, &written);
  if (r == GHOSTTY_OUT_OF_SPACE) { *needed = written; written = 0; }
  else if (r != GHOSTTY_SUCCESS) written = 0;
  ghostty_key_event_free(ev);
  ghostty_key_encoder_free(enc);
  return written;
}

bool hive_mode(GhosttyTerminal t, uint16_t number, bool ansi) {
  GhosttyTerminalModeConfig m = {.mode = ghostty_mode_new(number, ansi), .value = false};
  ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_MODE, &m);
  return m.value;
}

bool hive_paste_is_safe(GhosttyTerminal t, const char *data, size_t len) {
  if (hive_mode(t, 2004, false)) return true;
  return ghostty_paste_is_safe(data, len);
}

GhosttyResult hive_encode_paste(GhosttyTerminal t, char *data, size_t len, uint8_t *out, size_t cap, size_t *written) {
  return ghostty_paste_encode(data, len, hive_mode(t, 2004, false), (char *)out, cap, written);
}

void hive_set_render_hold(GhosttyTerminal t, GhosttyTerminalRenderHoldFn fn, void *userdata) {
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_USERDATA, userdata);
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_RENDER_HOLD, (const void *)fn);
}

void hive_scroll_delta(GhosttyTerminal t, intptr_t rows) {
  GhosttyTerminalScrollViewport s = {.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA, .value = {.delta = rows}};
  ghostty_terminal_scroll_viewport(t, s);
}

void hive_scroll_bottom(GhosttyTerminal t) {
  GhosttyTerminalScrollViewport s = {.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM};
  ghostty_terminal_scroll_viewport(t, s);
}

void hive_set_theme(GhosttyTerminal t, GhosttyColorRgb fg, GhosttyColorRgb bg, GhosttyColorRgb cursor,
                    const GhosttyColorRgb *palette16) {
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &fg);
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &bg);
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &cursor);
  GhosttyColorRgb palette[256];
  if (ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_COLOR_PALETTE_DEFAULT, palette) != GHOSTTY_SUCCESS) return;
  if (palette16) memcpy(palette, palette16, 16 * sizeof(GhosttyColorRgb));
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_COLOR_PALETTE, palette);
}

GhosttySelectionGesture hive_gesture_new(void) {
  GhosttySelectionGesture g = NULL;
  ghostty_selection_gesture_new(NULL, &g);
  return g;
}

void hive_gesture_free(GhosttySelectionGesture g, GhosttyTerminal t) {
  if (g) ghostty_selection_gesture_free(g, t);
}

void hive_select(GhosttyTerminal t, GhosttySelectionGesture g, int type, uint16_t col, uint16_t row,
                 double x, double y, uint32_t cols, uint32_t cell_width, uint32_t screen_height, uint64_t time_ns,
                 uint64_t repeat_interval_ns, double repeat_distance) {
  if (type < 0 || type > 2) return;
  GhosttySelectionGestureEventType types[] = {
    GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_PRESS, GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_RELEASE,
    GHOSTTY_SELECTION_GESTURE_EVENT_TYPE_DRAG,
  };
  GhosttySelectionGestureEvent ev = NULL;
  if (ghostty_selection_gesture_event_new(NULL, &ev, types[type]) != GHOSTTY_SUCCESS) return;
  GhosttyGridRef ref = GHOSTTY_INIT_SIZED(GhosttyGridRef);
  GhosttyPoint p = {.tag = GHOSTTY_POINT_TAG_VIEWPORT, .value = {.coordinate = {.x = col, .y = row}}};
  if (ghostty_terminal_grid_ref(t, p, &ref) == GHOSTTY_SUCCESS)
    ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REF, &ref);
  GhosttySurfacePosition pos = {.x = x, .y = y};
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_POSITION, &pos);
  GhosttySelectionGestureGeometry geo = {.columns = cols, .cell_width = cell_width, .padding_left = 0, .screen_height = screen_height};
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_GEOMETRY, &geo);
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_TIME_NS, &time_ns);
  // Without these every press is a single click.
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REPEAT_INTERVAL_NS, &repeat_interval_ns);
  ghostty_selection_gesture_event_set(ev, GHOSTTY_SELECTION_GESTURE_EVENT_OPT_REPEAT_DISTANCE, &repeat_distance);
  GhosttySelection sel = GHOSTTY_INIT_SIZED(GhosttySelection);
  GhosttyResult r = ghostty_selection_gesture_event(g, t, ev, &sel);
  if (r == GHOSTTY_SUCCESS) ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_SELECTION, &sel);
  ghostty_selection_gesture_event_free(ev);
}

bool hive_selection_text(GhosttyTerminal t, uint8_t **out, size_t *len) {
  GhosttySelection sel = GHOSTTY_INIT_SIZED(GhosttySelection);
  if (ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_SELECTION, &sel) != GHOSTTY_SUCCESS) return false;
  GhosttyTerminalSelectionFormatOptions o = GHOSTTY_INIT_SIZED(GhosttyTerminalSelectionFormatOptions);
  o.emit = GHOSTTY_FORMATTER_FORMAT_PLAIN;
  o.trim = true;
  o.unwrap = true;
  o.selection = &sel;
  return ghostty_terminal_selection_format_alloc(t, NULL, o, out, len) == GHOSTTY_SUCCESS && *len > 0;
}

void hive_clear_selection(GhosttyTerminal t) {
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_SELECTION, NULL);
}

int hive_wheel(GhosttyTerminal t, intptr_t rows, bool shift, float x, float y, HivePointerGeometry g,
               uint8_t *out, size_t cap, size_t *written) {
  *written = 0;
  if (shift || rows == 0) return 0;
  size_t n = (size_t)(rows < 0 ? -rows : rows);
  bool tracking = false;
  ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, &tracking);
  if (tracking) {
    GhosttyMouseEncoder enc = NULL;
    GhosttyMouseEvent ev = NULL;
    if (ghostty_mouse_encoder_new(NULL, &enc) != GHOSTTY_SUCCESS) return 1;
    if (ghostty_mouse_event_new(NULL, &ev) != GHOSTTY_SUCCESS) { ghostty_mouse_encoder_free(enc); return 1; }
    ghostty_mouse_encoder_setopt_from_terminal(enc, t);
    // Without a size the encoder assumes 1x1 px cells and every report
    // lands on the wrong cell.
    GhosttyMouseEncoderSize size = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
    size.screen_width = g.screen_width;
    size.screen_height = g.screen_height;
    size.cell_width = g.cell_width ? g.cell_width : 1;
    size.cell_height = g.cell_height ? g.cell_height : 1;
    ghostty_mouse_encoder_setopt(enc, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
    ghostty_mouse_event_set_action(ev, GHOSTTY_MOUSE_ACTION_PRESS);
    ghostty_mouse_event_set_button(ev, rows < 0 ? GHOSTTY_MOUSE_BUTTON_FOUR : GHOSTTY_MOUSE_BUTTON_FIVE);
    ghostty_mouse_event_set_position(ev, (GhosttyMousePosition){.x = x, .y = y});
    // One press per line, no release, as Ghostty sends them.
    for (size_t i = 0; i < n; i++) {
      size_t w = 0;
      if (ghostty_mouse_encoder_encode(enc, ev, (char *)out + *written, cap - *written, &w) != GHOSTTY_SUCCESS) break;
      *written += w;
    }
    ghostty_mouse_event_free(ev);
    ghostty_mouse_encoder_free(enc);
    return 1;
  }
  GhosttyTerminalScreen screen = GHOSTTY_TERMINAL_SCREEN_PRIMARY;
  ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN, &screen);
  if (screen != GHOSTTY_TERMINAL_SCREEN_ALTERNATE || !hive_mode(t, 1007, false)) return 0;
  for (size_t i = 0; i < n; i++) {
    size_t needed = 0;
    size_t w = hive_encode_key(t, rows < 0 ? GHOSTTY_KEY_ARROW_UP : GHOSTTY_KEY_ARROW_DOWN, 0, "", 0, 0,
                               out + *written, cap - *written, &needed);
    if (w == 0) break;
    *written += w;
  }
  return 1;
}

GhosttyMouseEncoder hive_mouse_encoder_new(void) {
  GhosttyMouseEncoder enc = NULL;
  if (ghostty_mouse_encoder_new(NULL, &enc) != GHOSTTY_SUCCESS) return NULL;
  bool track = true;
  ghostty_mouse_encoder_setopt(enc, GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL, &track);
  return enc;
}

void hive_mouse_encoder_free(GhosttyMouseEncoder enc) {
  if (enc) ghostty_mouse_encoder_free(enc);
}

intptr_t hive_mouse(GhosttyTerminal t, GhosttyMouseEncoder enc, int action, int button, GhosttyMods mods,
                    bool any_pressed, float x, float y, HivePointerGeometry g, uint8_t *out, size_t cap) {
  bool tracking = false;
  ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, &tracking);
  if (!tracking) return -1;
  if (!enc || action < 0 || action > 2) return 0;
  GhosttyMouseEvent ev = NULL;
  if (ghostty_mouse_event_new(NULL, &ev) != GHOSTTY_SUCCESS) return 0;
  ghostty_mouse_encoder_setopt_from_terminal(enc, t);
  GhosttyMouseEncoderSize size = GHOSTTY_INIT_SIZED(GhosttyMouseEncoderSize);
  size.screen_width = g.screen_width;
  size.screen_height = g.screen_height;
  size.cell_width = g.cell_width ? g.cell_width : 1;
  size.cell_height = g.cell_height ? g.cell_height : 1;
  ghostty_mouse_encoder_setopt(enc, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &size);
  ghostty_mouse_encoder_setopt(enc, GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED, &any_pressed);
  GhosttyMouseAction actions[] = {GHOSTTY_MOUSE_ACTION_PRESS, GHOSTTY_MOUSE_ACTION_RELEASE, GHOSTTY_MOUSE_ACTION_MOTION};
  ghostty_mouse_event_set_action(ev, actions[action]);
  if (button > 0) ghostty_mouse_event_set_button(ev, (GhosttyMouseButton)button);
  else ghostty_mouse_event_clear_button(ev);
  ghostty_mouse_event_set_mods(ev, mods);
  ghostty_mouse_event_set_position(ev, (GhosttyMousePosition){.x = x, .y = y});
  size_t written = 0;
  if (ghostty_mouse_encoder_encode(enc, ev, (char *)out, cap, &written) != GHOSTTY_SUCCESS) written = 0;
  ghostty_mouse_event_free(ev);
  return (intptr_t)written;
}
