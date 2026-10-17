<script>
  import { resolve } from '$app/paths';
  import { onMount } from 'svelte';

  // The prompt points at this site's own copy of the instructions, so it
  // stays right wherever the site is served from.
  let url = $state('https://manueltarouca.github.io/hive/agent-setup/prompt.md');
  onMount(() => { url = new URL(resolve('/agent-setup/prompt.md'), location.href).href; });
  const prompt = $derived(`Fetch and follow the instructions at ${url} to set me up with hive.`);

  let copied = $state(false);
  let timer;
  async function copy() {
    try {
      await navigator.clipboard.writeText(prompt);
    } catch {
      return; // no clipboard (an insecure page): the docs show the prompt as text
    }
    copied = true;
    clearTimeout(timer);
    timer = setTimeout(() => (copied = false), 2000);
  }
</script>

<button type="button" class="copy" class:copied onclick={copy} title="Sets up your coding agent for hive" aria-label="Copy a prompt that sets up your coding agent for hive">
  <svg viewBox="0 0 32 32" aria-hidden="true">
    <path d="M16 2.5 27.7 9.25v13.5L16 29.5 4.3 22.75V9.25Z" fill="var(--honey)" />
    <path d="m11.6 12.4 3.6 3.6-3.6 3.6M17.4 19.6h3.4" fill="none" stroke="var(--paper)" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
  </svg>
  <span class="text">
    <span class="idle">Copy agent prompt</span>
    <span class="done" aria-live="polite">{copied ? 'Prompt copied' : ''}</span>
  </span>
</button>

<style>
  .copy { display: inline-flex; align-items: center; gap: 10px; height: 44px; padding: 0 18px 0 12px; border: 1px solid var(--rule);
          border-radius: 22px; background: var(--card); color: var(--ink); font: 400 15px/1 var(--sans); letter-spacing: -0.01em;
          cursor: copy; white-space: nowrap; flex: none; transition: border-color 0.2s var(--ease); }
  .copy:hover { border-color: var(--honey); }
  .copy:focus-visible { outline: 2px solid var(--honey); outline-offset: 2px; }
  svg { width: 24px; height: 24px; flex: none; }
  /* Both labels share one cell, so the button keeps its width when it changes. */
  .text { display: inline-grid; text-align: left; }
  .idle, .done { grid-area: 1 / 1; transition: opacity 0.2s var(--ease); }
  .done { opacity: 0; color: var(--honey); }
  .copied .idle { opacity: 0; }
  .copied .done { opacity: 1; }
</style>
