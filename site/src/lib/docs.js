import { Marked } from 'marked';

// The docs are plain Markdown at the repository root: docs/*.md.
const files = import.meta.glob('../../../docs/*.md', { query: '?raw', import: 'default', eager: true });

function parse(raw) {
  const m = raw.match(/^---\n([\s\S]*?)\n---\n/);
  const meta = {};
  if (m) for (const line of m[1].split('\n')) {
    const i = line.indexOf(':');
    if (i > 0) meta[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  return { meta, body: m ? raw.slice(m[0].length) : raw };
}

export const docs = Object.entries(files)
  .map(([path, raw]) => {
    const slug = path.split('/').pop().replace(/\.md$/, '');
    const { meta, body } = parse(raw);
    return { slug, title: meta.title ?? slug, section: meta.section ?? 'Docs', order: Number(meta.order ?? 99),
             description: meta.description ?? '', body, raw };
  })
  .sort((a, b) => a.order - b.order);

export const sections = [...new Set(docs.map((d) => d.section))].map((name) => ({
  name, pages: docs.filter((d) => d.section === name)
}));

// Headings get GitHub's ids, so a link like ./remote#who-can-connect works
// on the site as it does on GitHub.
export function slugify(text) {
  return text.toLowerCase().trim().replace(/<[^>]*>/g, '').replace(/[^\p{L}\p{N}\s-]/gu, '').replace(/\s/g, '-');
}

function withIds() {
  let seen = new Map();
  return {
    hooks: { preprocess: (md) => { seen = new Map(); return md; } },
    renderer: {
      heading({ tokens, depth, text }) {
        const base = slugify(text);
        const n = seen.get(base) ?? 0;
        seen.set(base, n + 1);
        const id = n ? `${base}-${n}` : base;
        return `<h${depth} id="${id}">${this.parser.parseInline(tokens)}</h${depth}>\n`;
      }
    }
  };
}

const md = new Marked({ gfm: true }, withIds());

export function render(src) {
  // Drop the page's own h1: the layout shows the title.
  return md.parse(src.replace(/^\s*# .*\n/, ''));
}
