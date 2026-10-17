<script>
  let { size = 22 } = $props();
</script>

<span class="wordmark" style="--s: {size}px">
  <svg viewBox="0 0 32 32" aria-hidden="true"><path d="M16 2.5 27.7 9.25v13.5L16 29.5 4.3 22.75V9.25Z" fill="var(--honey)"/><path d="M16 9.5 21.6 12.75v6.5L16 22.5l-5.6-3.25v-6.5Z" fill="var(--paper)"/></svg>
  <b>hive</b>
</span>

<style>
  .wordmark { display: inline-flex; align-items: center; gap: calc(var(--s) * 0.32); color: var(--ink); }
  svg { width: var(--s); height: var(--s); }
  b { font: 600 calc(var(--s) * 0.92)/1 var(--sans); letter-spacing: -0.035em; }
</style>
