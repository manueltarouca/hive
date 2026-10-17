<script>
  import { resolve } from '$app/paths';
  import CopyPrompt from '#lib/CopyPrompt.svelte';
  let { data } = $props();
  let copied = $state(false);
  let timer;
  async function copyMarkdown() {
    try { await navigator.clipboard.writeText(data.raw); } catch { return; }
    copied = true;
    clearTimeout(timer);
    timer = setTimeout(() => (copied = false), 2000);
  }
</script>

<svelte:head>
  <title>{data.title} · hive docs</title>
  <meta name="description" content={data.description} />
  <link rel="alternate" type="text/markdown" href={resolve('/docs/[slug].md', { slug: data.slug })} />
</svelte:head>

<article>
  <h1>{data.title}</h1>
  <nav class="actions" aria-label="This page for agents">
    <button type="button" onclick={copyMarkdown}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5" /><path d="M10.5 3.5v-.5a1.5 1.5 0 0 0-1.5-1.5H4A1.5 1.5 0 0 0 2.5 3v5A1.5 1.5 0 0 0 4 9.5h.5" /></svg>
      <span class="swap"><span class:hide={copied}>Copy as Markdown</span><span class="ok" class:hide={!copied} aria-live="polite">{copied ? 'Copied' : ''}</span></span>
    </button>
    <a href={resolve('/docs/[slug].md', { slug: data.slug })}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="1.5" y="3.5" width="13" height="9" rx="1.5" /><path d="M4 10V6l1.75 2L7.5 6v4M10.5 6v4m0 0L9 8.5m1.5 1.5L12 8.5" /></svg>
      View as Markdown
    </a>
    {#if data.slug !== 'agent-setup'}
      <a href={resolve('/docs/[slug]', { slug: 'agent-setup' })}>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.5 13.6 4.75v6.5L8 14.5l-5.6-3.25v-6.5Z" /><path d="m6 6.5 1.75 1.5L6 9.5m3 0h1.5" /></svg>
        Agent setup
      </a>
    {/if}
  </nav>
  {#if data.slug === 'agent-setup'}<div class="cta"><CopyPrompt /></div>{/if}
  <div class="prose">{@html data.html}</div>
  <footer>
    {#if data.prev}<a href={resolve('/docs/[slug]', { slug: data.prev.slug })}><span>Previous</span>{data.prev.title}</a>{:else}<span></span>{/if}
    {#if data.next}<a class="next" href={resolve('/docs/[slug]', { slug: data.next.slug })}><span>Next</span>{data.next.title}</a>{/if}
  </footer>
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
  .prose :global(pre) { margin: 16px 0 0; padding: 12px 16px; background: var(--card); border: 1px solid var(--rule); border-radius: 12px; overflow-x: auto; font-size: 13px; line-height: 1.6; color: var(--ink); white-space: pre-wrap; overflow-wrap: anywhere; }
  .prose :global(table) { width: 100%; margin: 16px 0 0; border-collapse: collapse; font: 400 14px/1.45 var(--sans); color: var(--ink-62); }
  .prose :global(th) { text-align: left; font-weight: 400; color: var(--muted); }
  .prose :global(th), .prose :global(td) { padding: 8px 14px 8px 0; border-bottom: 1px solid var(--rule); vertical-align: top; }
  .prose :global(td code) { white-space: nowrap; }
  footer { display: flex; justify-content: space-between; gap: 16px; margin-top: 64px; padding-top: 20px; border-top: 1px solid var(--rule); }
  footer a { display: flex; flex-direction: column; gap: 4px; text-decoration: none; font: 400 15px/1.3 var(--sans); }
  footer a span { font-size: 13px; color: var(--muted); }
  footer .next { text-align: right; margin-left: auto; }
  .actions { display: flex; align-items: center; flex-wrap: wrap; margin: 14px 0 0; padding-bottom: 20px; border-bottom: 1px solid var(--rule);
             font: 400 13px/1 var(--sans); }
  .actions > * { display: inline-flex; align-items: center; gap: 6px; padding: 0; border: 0; background: none; color: var(--muted);
                 font: inherit; text-decoration: none; cursor: pointer; transition: color 0.15s var(--ease); }
  .actions > * + * { margin-left: 14px; padding-left: 14px; border-left: 1px solid var(--rule); }
  .actions > *:hover { color: var(--ink); }
  .actions svg { width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 1.2; stroke-linecap: round; stroke-linejoin: round; }
  .cta { margin: 24px 0 0; }
  .swap { display: inline-grid; text-align: left; }
  .swap > span { grid-area: 1 / 1; }
  .swap .hide { visibility: hidden; }
  .ok { color: var(--honey); }
</style>
