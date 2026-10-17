<script>
  import Mark from './Mark.svelte';
  let { size = 22 } = $props();
</script>

<span class="wordmark" style="--s: {size}px">
  <span class="mark"><Mark /></span>
  <b>hive</b>
</span>

<style>
  .wordmark { display: inline-flex; align-items: center; gap: calc(var(--s) * 0.32); color: var(--ink); }
  .mark { width: var(--s); height: var(--s); flex: none; }
  b { font: 600 calc(var(--s) * 0.92)/1 var(--sans); letter-spacing: -0.035em; }
</style>
