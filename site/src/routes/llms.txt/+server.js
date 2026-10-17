import { docs } from '#lib/docs.js';
import { resolve } from '$app/paths';
export const prerender = true;
export function GET({ url }) {
  const lines = ['# hive', '', '> A terminal multiplexer for agent-heavy work. Every docs page is also Markdown at the same address plus .md.', ''];
  let section = '';
  for (const d of docs) {
    if (d.section !== section) { section = d.section; lines.push('', `## ${section}`, ''); }
    lines.push(`- [${d.title}](${url.origin}${resolve('/docs/[slug].md', { slug: d.slug })}): ${d.description}`);
  }
  return new Response(lines.join('\n') + '\n', { headers: { 'content-type': 'text/plain; charset=utf-8' } });
}
