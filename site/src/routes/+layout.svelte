<script>
  import '../app.css';
  import { asset } from '$app/paths';
  import { page } from '$app/state';
  let { children } = $props();
  // Share cards need absolute addresses; page.url carries the public origin.
  const image = $derived(new URL(asset('/og.jpg'), page.url).href);
</script>

<svelte:head>
  <meta property="og:type" content="website" />
  <meta property="og:site_name" content="hive" />
  <meta property="og:url" content={page.url.href} />
  <meta property="og:image" content={image} />
  <meta property="og:image:width" content="1200" />
  <meta property="og:image:height" content="630" />
  <meta property="og:image:alt" content="hive: a terminal multiplexer for agent-heavy work. The app with its agents sidebar." />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="twitter:image" content={image} />
</svelte:head>

{@render children()}
