import { marked } from 'marked';

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

export function render(md) {
  // Drop the page's own h1: the layout shows the title.
  return marked.parse(md.replace(/^\s*# .*\n/, ''), { gfm: true });
}
