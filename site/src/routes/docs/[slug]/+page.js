import { error } from '@sveltejs/kit';
import { docs, render } from '#lib/docs.js';

export const entries = () => docs.map((d) => ({ slug: d.slug }));

export function load({ params }) {
  const i = docs.findIndex((d) => d.slug === params.slug);
  if (i < 0) error(404, 'No such page');
  const d = docs[i];
  return { title: d.title, description: d.description, slug: d.slug, raw: d.body.trimStart(), html: render(d.body),
           prev: docs[i - 1] ?? null, next: docs[i + 1] ?? null };
}
