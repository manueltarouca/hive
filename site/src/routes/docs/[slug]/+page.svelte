<script>
  import { resolve } from '$app/paths';
  let { data } = $props();
</script>

<svelte:head>
  <title>{data.title} · hive docs</title>
  <meta name="description" content={data.description} />
  <link rel="alternate" type="text/markdown" href={resolve('/docs/[slug].md', { slug: data.slug })} />
</svelte:head>

<article>
  <h1>{data.title}</h1>
  <div class="prose">{@html data.html}</div>
  <footer>
    {#if data.prev}<a href={resolve('/docs/[slug]', { slug: data.prev.slug })}><span>Previous</span>{data.prev.title}</a>{:else}<span></span>{/if}
    {#if data.next}<a class="next" href={resolve('/docs/[slug]', { slug: data.next.slug })}><span>Next</span>{data.next.title}</a>{/if}
  </footer>
  <p class="md">This page as <a href={resolve('/docs/[slug].md', { slug: data.slug })}>Markdown</a>.</p>
</article>

<style>
  h1 { font: 400 28px/1.15 var(--sans); letter-spacing: -0.02em; margin: 0 0 8px; }
  .prose :global(h2) { font: 400 18px/1.3 var(--sans); letter-spacing: -0.015em; margin: 40px 0 0; }
  .prose :global(p), .prose :global(li) { font: 400 15px/1.5 var(--sans); color: var(--ink-62); }
  .prose :global(p) { margin: 16px 0 0; }
  .prose :global(ul), .prose :global(ol) { margin: 16px 0 0; padding-left: 20px; }
  .prose :global(li) { margin: 4px 0; }
  .prose :global(li::marker) { color: var(--muted); }
  .prose :global(strong) { color: var(--ink); font-weight: 500; }
  .prose :global(a) { color: var(--ink); text-decoration-color: var(--honey); text-underline-offset: 3px; text-decoration-thickness: 1px; }
  .prose :global(:not(pre) > code) { font-size: 0.875em; color: var(--ink); background: var(--tint); padding: 0.09em 0.31em; border-radius: 4px; }
  .prose :global(pre) { margin: 16px 0 0; padding: 12px 16px; background: var(--card); border: 1px solid var(--rule); border-radius: 12px; overflow-x: auto; font-size: 13px; line-height: 1.6; color: var(--ink); }
  .prose :global(table) { width: 100%; margin: 16px 0 0; border-collapse: collapse; font: 400 14px/1.45 var(--sans); color: var(--ink-62); }
  .prose :global(th) { text-align: left; font-weight: 400; color: var(--muted); }
  .prose :global(th), .prose :global(td) { padding: 8px 14px 8px 0; border-bottom: 1px solid var(--rule); vertical-align: top; }
  .prose :global(td code) { white-space: nowrap; }
  footer { display: flex; justify-content: space-between; gap: 16px; margin-top: 64px; padding-top: 20px; border-top: 1px solid var(--rule); }
  footer a { display: flex; flex-direction: column; gap: 4px; text-decoration: none; font: 400 15px/1.3 var(--sans); }
  footer a span { font-size: 13px; color: var(--muted); }
  footer .next { text-align: right; margin-left: auto; }
  .md { font: 400 13px/1.4 var(--sans); color: var(--muted); margin-top: 24px; }
  .md a { color: inherit; }
</style>
