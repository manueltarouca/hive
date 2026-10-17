<script>
  import { page } from '$app/state';
  import { resolve } from '$app/paths';
  import { sections } from '#lib/docs.js';
  import Wordmark from '#lib/Wordmark.svelte';
  let { children } = $props();
</script>

<div class="docs">
  <aside>
    <a class="home" href={resolve('/')} aria-label="hive home"><Wordmark /><span>Docs</span></a>
    <nav>
      {#each sections as s}
        <h2>{s.name}</h2>
        {#each s.pages as p}
          <a href={resolve('/docs/[slug]', { slug: p.slug })} aria-current={page.url.pathname.endsWith('/docs/' + p.slug) ? 'page' : undefined}>{p.title}</a>
        {/each}
      {/each}
    </nav>
  </aside>
  <main>{@render children()}</main>
</div>

<style>
  .docs { display: grid; grid-template-columns: 176px minmax(0, 576px); gap: 0 96px; padding: 0 80px; max-width: 1100px; }
  aside { position: sticky; top: 0; align-self: start; height: 100vh; overflow-y: auto; padding: 28px 0 40px; }
  .home { display: flex; align-items: center; gap: 10px; text-decoration: none; margin: 0 0 28px; }
  .home span { font: 400 14px/1 var(--sans); color: var(--muted); }
  nav { display: flex; flex-direction: column; gap: 1px; font: 400 14px/1.2 var(--sans); letter-spacing: -0.01em; }
  nav h2 { font: inherit; color: var(--muted); margin: 22px 0 8px; }
  nav h2:first-child { margin-top: 0; }
  nav a { color: var(--ink-62); text-decoration: none; padding: 5px 8px; margin: 0 -8px; border-radius: 6px; transition: color 0.15s var(--ease), background 0.15s var(--ease); }
  nav a:hover { color: var(--ink); background: var(--tint); }
  nav a[aria-current='page'] { color: var(--ink); background: var(--honey-soft); }
  main { padding: 64px 0 120px; min-width: 0; }
  @media (max-width: 820px) {
    .docs { grid-template-columns: 1fr; padding: 0 24px; }
    aside { position: static; height: auto; padding-bottom: 0; }
    main { padding-top: 24px; }
  }
</style>
