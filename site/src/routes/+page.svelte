<script>
  import { asset, resolve } from '$app/paths';
  import { onMount } from 'svelte';
  import Wordmark from '#lib/Wordmark.svelte';
  import CopyPrompt from '#lib/CopyPrompt.svelte';

  // What hive is for, one at a time.
  const uses = ['coding agents.', 'long builds.', 'dev servers.', 'test runs.', 'all your sessions.'];
  let current = $state(0);
  onMount(() => {
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const t = setInterval(() => (current = (current + 1) % uses.length), 2200);
    return () => clearInterval(t);
  });
</script>

<svelte:head>
  <title>hive · a terminal multiplexer for agent-heavy work</title>
  <meta name="description" content="hive keeps your terminals running, shows which coding agents need you, and brings everything back after a restart." />
  <meta property="og:title" content="hive · a terminal multiplexer for agent-heavy work" />
  <meta property="og:description" content="hive keeps your terminals running, shows which coding agents need you, and brings everything back after a restart." />
</svelte:head>

<div class="page">
  <main>
    <header><Wordmark size={26} /></header>

    <h1>
      <span>A terminal multiplexer for</span>
      <span class="wheel" aria-live="polite">
        {#each uses as u, i}
          <span class="use" class:on={i === current} aria-hidden={i !== current}>{u}</span>
        {/each}
      </span>
    </h1>

    <a class="badge" href={resolve('/docs/[slug]', { slug: 'installation' })}>
      <svg viewBox="0 0 32 32" aria-hidden="true"><path d="M16 2.5 27.7 9.25v13.5L16 29.5 4.3 22.75V9.25Z" fill="var(--honey)"/><path d="M16 9.5 21.6 12.75v6.5L16 22.5l-5.6-3.25v-6.5Z" fill="var(--paper)"/></svg>
      <p>hive is open source.</p>
      <span>Read the docs</span>
    </a>
    <div class="agent">
      <CopyPrompt />
      <p>Paste it into Claude Code, Codex or any agent. It installs hive and teaches the agent to use it.</p>
    </div>

    <section class="essay">
      <p>
        More and more terminal work runs at the same time: coding agents on different tasks, a dev
        server, tests. When you close the window, the work stops. When you restart your computer, you
        set up everything again by hand. And an agent can wait an hour for your answer, in a tab you
        don't see.
      </p>
      <p>
        tmux helps with some of this, and many of us still use it. But it draws a terminal inside your
        terminal. You need a special mode to scroll. And programs can't easily control it.
      </p>
      <p>
        hive is a terminal multiplexer for this kind of work. Your sessions keep running when you close
        the app, and they come back after a restart. Scrolling and selecting text work like in any Mac
        app. Programs can control everything. Coding agents are part of the design.
      </p>
    </section>



    <section>
      <p class="label">What it does</p>
      <ol>
        <li><span>1</span><div><b>Keeps your sessions.</b> Close the app, stop the server, or restart your computer. Your layouts, your scrollback and your folders come back.</div></li>
        <li><span>2</span><div><b>Knows your agents.</b> Claude Code tells hive what it is doing and when it needs you. You see every agent in one list, the waiting ones first. After a restart, each agent continues its conversation.</div></li>
        <li><span>3</span><div><b>Lets programs control it.</b> Everything you do in the app is also an API call. The command line, your scripts and your agents all use the same API.</div></li>
        <li><span>4</span><div><b>Feels like a normal terminal.</b> hive uses Ghostty's terminal engine. Scrolling, scrollback and text selection are native. There is no copy mode.</div></li>
      </ol>
    </section>

    <section class="essay">
      <p class="label">Your data</p>
      <p>
        hive runs on your computer and connects to nothing else. There is no account and no
        telemetry. There is nothing to sign up for.
      </p>
    </section>

    <section class="start">
      <p class="label">Try it</p>
<pre><code>git clone https://github.com/manueltarouca/hive && cd hive
make vt build
./bin/hive new work</code></pre>
      <p class="more"><a href={resolve('/docs/[slug]', { slug: 'installation' })}>Installation</a> · <a href={resolve('/docs/[slug]', { slug: 'agents' })}>Agents</a> · <a href={resolve('/docs/[slug]', { slug: 'cli' })}>The CLI</a></p>
    </section>

    <footer>
      <a href="https://github.com/manueltarouca/hive">GitHub</a>
      <span class="rule"></span>
      <a href={resolve('/docs/[slug]', { slug: 'installation' })}>Docs</a>
      <a href={resolve('/llms.txt')}>llms.txt</a>
    </footer>
  </main>

  <aside class="panel" aria-label="hive, running">
    <picture>
      <source srcset={asset('/img/window-dark.webp')} media="(prefers-color-scheme: dark)" />
      <img src={asset('/img/window-light.webp')} alt="hive with three agents in the sidebar, one blocked on a permission prompt, and a session split into a shell, a Claude Code pane and a test run." width="2200" height="1460" />
    </picture>
  </aside>
</div>

<style>
  .page { display: grid; grid-template-columns: minmax(0, 72ch) minmax(0, 1fr); gap: 64px; padding: 48px 0 40px 64px; }
  main { min-width: 0; }

  /* The product, large, in a sticky panel that the essay scrolls past. */
  .panel { position: sticky; top: 8px; height: calc(100vh - 16px); border-radius: 22px 0 0 22px; overflow: hidden;
           background: linear-gradient(160deg, #f6d9a3 0%, #f3e6d1 48%, #efe9df 100%); }
  .panel img { position: absolute; top: 50%; left: 8%; width: 165%; max-width: none; height: auto; transform: translateY(-50%); }
  @media (prefers-color-scheme: dark) {
    .panel { background: linear-gradient(160deg, #3b2c17 0%, #1e1b1f 52%, #151418 100%); }
  }
  header { margin-bottom: 18vh; }

  h1 { font: 500 clamp(26px, 2.1vw, 32px)/1.15 var(--sans); letter-spacing: -0.02em; margin: 0 0 12vh; display: flex; flex-wrap: wrap; gap: 0 0.28em; }
  .wheel { position: relative; display: inline-grid; color: var(--honey); }
  .use { grid-area: 1 / 1; opacity: 0; transform: translateY(0.45em); filter: blur(3px);
         transition: opacity 0.5s var(--ease), transform 0.5s var(--ease), filter 0.5s var(--ease); white-space: nowrap; }
  .use.on { opacity: 1; transform: none; filter: none; }

  .badge { display: flex; align-items: center; gap: 16px; padding: 14px 20px 14px 14px; border: 1px solid var(--rule); border-radius: 28px;
           text-decoration: none; font-size: clamp(17px, 1.47vw, 21px); line-height: 1.3; letter-spacing: -0.01em; transition: border-color 0.2s var(--ease); }
  .badge:hover { border-color: var(--honey); }
  .badge svg { width: 44px; height: 44px; flex: none; }
  .badge p { margin: 0; flex: 1; }
  .badge span { color: var(--honey); }
  .agent { display: flex; align-items: center; gap: 16px; margin-top: 14px; }
  .agent p { margin: 0; font-size: 14px; line-height: 1.4; color: var(--muted); letter-spacing: 0; }
  @media (max-width: 720px) { .agent { flex-direction: column; align-items: flex-start; } }

  section { margin-top: 56px; }
  p, li { font-size: clamp(17px, 1.47vw, 21px); line-height: 1.3; letter-spacing: -0.01em; }
  .essay p { margin: 0 0 20px; }
  .label { color: var(--muted); margin: 0 0 16px; }
  a { text-decoration-color: var(--rule); text-underline-offset: 3px; }
  a:hover { text-decoration-color: var(--honey); }

  ol { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 14px; }
  li { display: flex; gap: 16px; margin: 0; }
  li span { width: 16px; flex: none; color: var(--muted); font-variant-numeric: tabular-nums; }
  li b { font-weight: 500; }

  .shot { margin: 48px 0 0; border-radius: 16px; overflow: hidden; background: linear-gradient(160deg, #f6d9a3, #efe9df); }
  .shot img { width: 100%; height: auto; }

  .start pre { margin: 0; padding: 16px 20px; background: var(--card); border: 1px solid var(--rule); border-radius: 12px;
               font-size: 14px; line-height: 1.7; overflow-x: auto; }
  .more { margin-top: 16px; color: var(--muted); }

  footer { display: flex; align-items: center; gap: 16px; margin-top: 120px; font: 400 12px/1 var(--mono); }
  footer a { text-decoration: none; }
  .rule { flex: 1; height: 1px; background: var(--rule); }

  @media (max-width: 1100px) {
    .page { grid-template-columns: 1fr; padding-right: 64px; }
    .panel { position: relative; top: 0; height: 56vw; border-radius: 18px; }
    .panel img { left: 6%; width: 130%; }
  }
  @media (max-width: 720px) {
    .page { padding: 28px 22px; }
    header { margin-bottom: 64px; }
    h1 { margin-bottom: 56px; }
  }
</style>
