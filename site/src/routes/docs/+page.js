import { redirect } from '@sveltejs/kit';
import { resolve } from '$app/paths';
import { docs } from '#lib/docs.js';
export const load = () => redirect(307, resolve('/docs/[slug]', { slug: docs[0].slug }));
