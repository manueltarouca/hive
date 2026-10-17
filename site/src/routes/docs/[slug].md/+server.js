import { docs } from '#lib/docs.js';
export const prerender = true;
export const entries = () => docs.map((d) => ({ slug: d.slug }));
export function GET({ params }) {
  const d = docs.find((x) => x.slug === params.slug);
  return new Response(d ? d.raw : 'not found', { status: d ? 200 : 404, headers: { 'content-type': 'text/markdown; charset=utf-8' } });
}
