// The JNI side of xyz.matm.hive.vt.Vt: one libghostty terminal per mirrored
// block, driven through the Mac app's shim. Not thread-safe: Kotlin calls
// each terminal from one thread (the vt thread).

#include <jni.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include "CGhosttyVT.h"

typedef struct {
  GhosttyTerminal t;
  GhosttyRenderState rs;
  GhosttyRenderStateRowIterator it;
  GhosttyRenderStateRowCells cells;
  GhosttyMouseEncoder mouse;
  HiveCell *scratch;
  size_t scratch_cap;
  uint8_t *out;  // the last frame, handed to Kotlin as a direct buffer
  size_t out_cap;
  bool held;
  int64_t held_since_ms;
  bool framed;  // a frame has been read: a held terminal may return none
} Term;

static int64_t now_ms(void) {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

static void on_hold(GhosttyTerminal t, void *userdata, bool held) {
  (void)t;
  Term *m = userdata;
  m->held = held;
  m->held_since_ms = held ? now_ms() : 0;
}

static Term *wrap(GhosttyTerminal t) {
  Term *m = calloc(1, sizeof(Term));
  if (!m) { ghostty_terminal_free(t); return NULL; }
  m->t = t;
  // A phone keeps less scrollback than the Mac (16 MB).
  size_t scrollback = 4u << 20;
  ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_BYTES, &scrollback);
  if (ghostty_render_state_new(NULL, &m->rs) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_iterator_new(NULL, &m->it) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_cells_new(NULL, &m->cells) != GHOSTTY_SUCCESS) {
    if (m->cells) ghostty_render_state_row_cells_free(m->cells);
    if (m->it) ghostty_render_state_row_iterator_free(m->it);
    if (m->rs) ghostty_render_state_free(m->rs);
    ghostty_terminal_free(t);
    free(m);
    return NULL;
  }
  hive_set_render_hold(t, on_hold, m);
  // A snapshot taken during synchronized output arrives already held; the
  // callback only reports transitions.
  if (hive_mode(t, 2026, false)) { m->held = true; m->held_since_ms = now_ms(); }
  return m;
}

#define FN(name) Java_xyz_matm_hive_vt_Vt_##name
#define T ((Term *)(intptr_t)h)

JNIEXPORT jlong JNICALL FN(nNew)(JNIEnv *env, jclass c, jint cols, jint rows) {
  GhosttyTerminal t = NULL;
  if (hive_terminal_new((uint16_t)cols, (uint16_t)rows, &t) != GHOSTTY_SUCCESS) return 0;
  return (jlong)(intptr_t)wrap(t);
}

JNIEXPORT jlong JNICALL FN(nFromSnapshot)(JNIEnv *env, jclass c, jbyteArray data) {
  jsize n = (*env)->GetArrayLength(env, data);
  jbyte *p = (*env)->GetByteArrayElements(env, data, NULL);
  GhosttyTerminal t = NULL;
  GhosttyResult r = hive_terminal_from_snapshot((const uint8_t *)p, (size_t)n, &t);
  (*env)->ReleaseByteArrayElements(env, data, p, JNI_ABORT);
  if (r != GHOSTTY_SUCCESS) return 0;
  return (jlong)(intptr_t)wrap(t);
}

JNIEXPORT void JNICALL FN(nFree)(JNIEnv *env, jclass c, jlong h) {
  Term *m = T;
  if (!m) return;
  hive_set_render_hold(m->t, NULL, NULL);
  hive_mouse_encoder_free(m->mouse);
  ghostty_render_state_row_cells_free(m->cells);
  ghostty_render_state_row_iterator_free(m->it);
  ghostty_render_state_free(m->rs);
  ghostty_terminal_free(m->t);
  free(m->scratch);
  free(m->out);
  free(m);
}

JNIEXPORT void JNICALL FN(nFeed)(JNIEnv *env, jclass c, jlong h, jbyteArray data, jint off, jint len) {
  jbyte *p = (*env)->GetByteArrayElements(env, data, NULL);
  ghostty_terminal_vt_write(T->t, (const uint8_t *)p + off, (size_t)len);
  (*env)->ReleaseByteArrayElements(env, data, p, JNI_ABORT);
}

JNIEXPORT void JNICALL FN(nResize)(JNIEnv *env, jclass c, jlong h, jint cols, jint rows, jint cw, jint ch) {
  ghostty_terminal_resize(T->t, (uint16_t)cols, (uint16_t)rows, cw > 0 ? (uint32_t)cw : 1, ch > 0 ? (uint32_t)ch : 1);
}

JNIEXPORT void JNICALL FN(nSetTheme)(JNIEnv *env, jclass c, jlong h, jint fg, jint bg, jint cursor, jintArray palette) {
  GhosttyColorRgb pal[16];
  jint *p = (*env)->GetIntArrayElements(env, palette, NULL);
  for (int i = 0; i < 16; i++) pal[i] = (GhosttyColorRgb){(uint8_t)(p[i] >> 16), (uint8_t)(p[i] >> 8), (uint8_t)p[i]};
  (*env)->ReleaseIntArrayElements(env, palette, p, JNI_ABORT);
#define RGB(x) ((GhosttyColorRgb){(uint8_t)((x) >> 16), (uint8_t)((x) >> 8), (uint8_t)(x)})
  hive_set_theme(T->t, RGB(fg), RGB(bg), RGB(cursor), pal);
}

static bool reserve(Term *m, size_t need) {
  if (need <= m->out_cap) return true;
  size_t cap = need * 2;
  uint8_t *p = realloc(m->out, cap);
  if (!p) return false;
  m->out = p;
  m->out_cap = cap;
  return true;
}

static void put16(uint8_t *p, uint16_t v) { p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); }
static void put32(uint8_t *p, uint32_t v) { for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i)); }
static uint32_t rgb(GhosttyColorRgb c) { return (uint32_t)c.r << 16 | (uint32_t)c.g << 8 | c.b; }

// The rows that changed since the last frame, as a direct buffer (valid
// until the next call), or null while synchronized output holds the
// picture. Little endian:
//   i32 cols, i32 rows, u8 full, u8 cursor visible, u8 in viewport,
//   u8 blinking, u16 x, u16 y, u8 style, u8 0, u16 0, i32 bg, i32 fg,
//   i32 dirty row count; then per row: u16 y, u16 cells, and per cell:
//   u8 text length, the UTF-8 text, u8 wide, u8 flags (bold 1, italic 2,
//   faint 4, inverse 8, invisible 16, strikethrough 32, blink 64,
//   selected 128), u8 underline, u8 colors (fg 1, bg 2), then fg and bg
//   as three bytes each when present.
JNIEXPORT jobject JNICALL FN(nFrame)(JNIEnv *env, jclass c, jlong h) {
  Term *m = T;
  if (m->held && m->framed && now_ms() - m->held_since_ms < 1000) return NULL;
  ghostty_render_state_update(m->rs, m->t);
  HiveFrameMeta meta;
  hive_frame_meta(m->rs, &meta);
  size_t cols = meta.cols, rows = meta.rows;
  bool full = !m->framed || meta.dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL;
  if (m->scratch_cap < cols) {
    free(m->scratch);
    m->scratch = calloc(cols, sizeof(HiveCell));
    m->scratch_cap = m->scratch ? cols : 0;
    if (!m->scratch) { m->framed = false; return NULL; }
  }
  const size_t head = 36;
  // Out of memory: no frame, and the next one is full (rows already read are marked clean).
  if (!reserve(m, head + rows * (4 + cols * 16))) { m->framed = false; return NULL; }
  size_t at = head;
  uint32_t ndirty = 0;
  ghostty_render_state_get(m->rs, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &m->it);
  for (size_t y = 0; y < rows && ghostty_render_state_row_iterator_next(m->it); y++) {
    if (!full && !hive_row_dirty(m->it)) continue;
    size_t n = hive_row_cells(m->it, m->cells, m->scratch, cols);
    hive_row_set_clean(m->it);
    // Worst case per cell: 1 + 63 + 4 + 6.
    if (!reserve(m, at + 4 + n * 74 + rows * 4)) { m->framed = false; return NULL; }
    put16(m->out + at, (uint16_t)y);
    put16(m->out + at + 2, (uint16_t)n);
    at += 4;
    for (size_t i = 0; i < n; i++) {
      HiveCell *k = &m->scratch[i];
      uint8_t len = k->text_len > 63 ? 63 : k->text_len;
      m->out[at++] = len;
      memcpy(m->out + at, k->text, len);
      at += len;
      m->out[at++] = k->wide;
      m->out[at++] = (uint8_t)(k->bold | k->italic << 1 | k->faint << 2 | k->inverse << 3 | k->invisible << 4 |
                               k->strikethrough << 5 | k->blink << 6 | k->selected << 7);
      m->out[at++] = k->underline;
      m->out[at++] = (uint8_t)(k->has_fg | k->has_bg << 1);
      if (k->has_fg) { m->out[at++] = k->fg.r; m->out[at++] = k->fg.g; m->out[at++] = k->fg.b; }
      if (k->has_bg) { m->out[at++] = k->bg.r; m->out[at++] = k->bg.g; m->out[at++] = k->bg.b; }
    }
    ndirty++;
  }
  ghostty_render_state_clean(m->rs);
  uint8_t *o = m->out;
  put32(o, (uint32_t)cols);
  put32(o + 4, (uint32_t)rows);
  o[8] = full;
  o[9] = meta.cursor_visible;
  o[10] = meta.cursor_in_viewport;
  o[11] = meta.cursor_blinking;
  put16(o + 12, meta.cursor_x);
  put16(o + 14, meta.cursor_y);
  o[16] = meta.cursor_style;
  o[17] = 0;
  put16(o + 18, 0);
  put32(o + 20, rgb(meta.background));
  put32(o + 24, rgb(meta.foreground));
  put32(o + 28, ndirty);
  put32(o + 32, 0);
  m->framed = true;
  return (*env)->NewDirectByteBuffer(env, m->out, (jlong)at);
}

static jbyteArray bytes(JNIEnv *env, const uint8_t *p, size_t n) {
  jbyteArray a = (*env)->NewByteArray(env, (jsize)n);
  if (a && n) (*env)->SetByteArrayRegion(env, a, 0, (jsize)n, (const jbyte *)p);
  return a;
}

// Android key codes (android.view.KeyEvent) to libghostty's physical keys.
static GhosttyKey key_for(int code) {
  if (code >= 29 && code <= 54) return (GhosttyKey)(GHOSTTY_KEY_A + (code - 29));
  if (code >= 7 && code <= 16) return (GhosttyKey)(GHOSTTY_KEY_DIGIT_0 + (code - 7));
  if (code >= 131 && code <= 142) return (GhosttyKey)(GHOSTTY_KEY_F1 + (code - 131));
  switch (code) {
    case 66: return GHOSTTY_KEY_ENTER;
    case 160: return GHOSTTY_KEY_NUMPAD_ENTER;
    case 61: return GHOSTTY_KEY_TAB;
    case 62: return GHOSTTY_KEY_SPACE;
    case 67: return GHOSTTY_KEY_BACKSPACE;
    case 112: return GHOSTTY_KEY_DELETE;
    case 111: return GHOSTTY_KEY_ESCAPE;
    case 19: return GHOSTTY_KEY_ARROW_UP;
    case 20: return GHOSTTY_KEY_ARROW_DOWN;
    case 21: return GHOSTTY_KEY_ARROW_LEFT;
    case 22: return GHOSTTY_KEY_ARROW_RIGHT;
    case 122: return GHOSTTY_KEY_HOME;
    case 123: return GHOSTTY_KEY_END;
    case 92: return GHOSTTY_KEY_PAGE_UP;
    case 93: return GHOSTTY_KEY_PAGE_DOWN;
    case 124: return GHOSTTY_KEY_INSERT;
    case 68: return GHOSTTY_KEY_BACKQUOTE;
    case 69: return GHOSTTY_KEY_MINUS;
    case 70: return GHOSTTY_KEY_EQUAL;
    case 71: return GHOSTTY_KEY_BRACKET_LEFT;
    case 72: return GHOSTTY_KEY_BRACKET_RIGHT;
    case 73: return GHOSTTY_KEY_BACKSLASH;
    case 74: return GHOSTTY_KEY_SEMICOLON;
    case 75: return GHOSTTY_KEY_QUOTE;
    case 76: return GHOSTTY_KEY_SLASH;
    case 55: return GHOSTTY_KEY_COMMA;
    case 56: return GHOSTTY_KEY_PERIOD;
    default: return GHOSTTY_KEY_UNIDENTIFIED;
  }
}

// text: UTF-8 bytes (not a jstring: JNI's modified UTF-8 splits emoji into surrogates).
JNIEXPORT jbyteArray JNICALL FN(nEncodeKey)(JNIEnv *env, jclass c, jlong h, jint code, jint mods, jint consumed,
                                           jbyteArray text, jint unshifted) {
  jsize tn = (*env)->GetArrayLength(env, text);
  char *s = malloc((size_t)tn + 1);
  if (!s) return NULL;
  (*env)->GetByteArrayRegion(env, text, 0, tn, (jbyte *)s);
  s[tn] = 0;
  size_t slen = (size_t)tn;
  uint8_t buf[128];
  size_t needed = 0;
  size_t n = hive_encode_key(T->t, key_for(code), (GhosttyMods)mods, (GhosttyMods)consumed, s, slen, (uint32_t)unshifted,
                             buf, sizeof buf, &needed);
  jbyteArray out;
  if (needed > 0) {
    // Long input method text: once more at the size it needs.
    uint8_t *big = malloc(needed);
    size_t again = 0;
    n = big ? hive_encode_key(T->t, key_for(code), (GhosttyMods)mods, (GhosttyMods)consumed, s, slen,
                              (uint32_t)unshifted, big, needed, &again) : 0;
    out = bytes(env, big, n);
    free(big);
  } else {
    out = bytes(env, buf, n);
  }
  free(s);
  return out;
}

JNIEXPORT jbyteArray JNICALL FN(nEncodePaste)(JNIEnv *env, jclass c, jlong h, jbyteArray utf8) {
  jsize n = (*env)->GetArrayLength(env, utf8);
  char *data = malloc((size_t)n + 1);
  uint8_t *out = malloc((size_t)n * 2 + 32);
  if (!data || !out) { free(data); free(out); return NULL; }
  (*env)->GetByteArrayRegion(env, utf8, 0, n, (jbyte *)data);
  size_t written = 0;
  jbyteArray r = NULL;
  if (hive_encode_paste(T->t, data, (size_t)n, out, (size_t)n * 2 + 32, &written) == GHOSTTY_SUCCESS)
    r = bytes(env, out, written);
  free(data);
  free(out);
  return r;
}

JNIEXPORT jboolean JNICALL FN(nPasteSafe)(JNIEnv *env, jclass c, jlong h, jbyteArray utf8) {
  jsize n = (*env)->GetArrayLength(env, utf8);
  jbyte *p = (*env)->GetByteArrayElements(env, utf8, NULL);
  bool ok = hive_paste_is_safe(T->t, (const char *)p, (size_t)n);
  (*env)->ReleaseByteArrayElements(env, utf8, p, JNI_ABORT);
  return ok;
}

JNIEXPORT void JNICALL FN(nScroll)(JNIEnv *env, jclass c, jlong h, jint rows) { hive_scroll_delta(T->t, rows); }
JNIEXPORT void JNICALL FN(nScrollBottom)(JNIEnv *env, jclass c, jlong h) { hive_scroll_bottom(T->t); }

static HivePointerGeometry geometry(jint sw, jint sh, jint cw, jint ch) {
  return (HivePointerGeometry){(uint32_t)(sw > 0 ? sw : 1), (uint32_t)(sh > 0 ? sh : 1), (uint32_t)(cw > 0 ? cw : 1),
                               (uint32_t)(ch > 0 ? ch : 1)};
}

// null: the caller scrolls its own viewport. Otherwise the bytes for the
// program (maybe none).
JNIEXPORT jbyteArray JNICALL FN(nWheel)(JNIEnv *env, jclass c, jlong h, jint rows, jfloat x, jfloat y, jint sw, jint sh,
                                       jint cw, jint ch) {
  int lines = rows < -64 ? -64 : rows > 64 ? 64 : rows;
  uint8_t buf[64 * 32];
  size_t written = 0;
  if (hive_wheel(T->t, lines, false, x, y, geometry(sw, sh, cw, ch), buf, sizeof buf, &written) == 0) return NULL;
  return bytes(env, buf, written);
}

// A tap for a program that tracks the mouse: press or release of the left
// button. null when the program doesn't track the mouse.
JNIEXPORT jbyteArray JNICALL FN(nMouse)(JNIEnv *env, jclass c, jlong h, jint action, jfloat x, jfloat y, jint sw, jint sh,
                                       jint cw, jint ch) {
  Term *m = T;
  if (!hive_mouse_tracking(m->t)) return NULL;
  if (!m->mouse) m->mouse = hive_mouse_encoder_new();
  uint8_t buf[64];
  intptr_t n = hive_mouse(m->t, m->mouse, action, action == 2 ? 0 : 1, 0, false, x, y, geometry(sw, sh, cw, ch), buf,
                          sizeof buf);
  if (n < 0) return NULL;
  return bytes(env, buf, (size_t)n);
}

JNIEXPORT jstring JNICALL FN(nHyperlink)(JNIEnv *env, jclass c, jlong h, jint col, jint row) {
  uint8_t buf[4096 + 1];
  size_t len = 0;
  if (hive_hyperlink_at(T->t, (uint16_t)col, (uint16_t)row, buf, sizeof buf - 1, &len) != GHOSTTY_SUCCESS || len == 0)
    return NULL;
  buf[len] = 0;
  return (*env)->NewStringUTF(env, (const char *)buf);
}

JNIEXPORT jstring JNICALL FN(nPlainText)(JNIEnv *env, jclass c, jlong h) {
  uint8_t *p = NULL;
  size_t len = 0;
  if (hive_format_plain(T->t, &p, &len) != GHOSTTY_SUCCESS || !p) return (*env)->NewStringUTF(env, "");
  jbyteArray a = bytes(env, p, len);
  hive_free(p, len);
  // Through java.lang.String(byte[], "UTF-8"): NewStringUTF wants modified
  // UTF-8, which emoji are not.
  jclass sc = (*env)->FindClass(env, "java/lang/String");
  jmethodID ctor = (*env)->GetMethodID(env, sc, "<init>", "([BLjava/lang/String;)V");
  return (jstring)(*env)->NewObject(env, sc, ctor, a, (*env)->NewStringUTF(env, "UTF-8"));
}

JNIEXPORT jboolean JNICALL FN(nMode)(JNIEnv *env, jclass c, jlong h, jint number, jboolean ansi) {
  return hive_mode(T->t, (uint16_t)number, ansi);
}
