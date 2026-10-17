import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [
    sveltekit({
      adapter: adapter({ fallback: '404.html' }),
      // GitHub Pages serves the project at /hive.
      // origin: absolute links in prerendered files (llms.txt) use the public address.
      paths: { base: process.env.BASE_PATH ?? '', origin: process.env.SITE_ORIGIN ?? 'https://manueltarouca.github.io' },
      prerender: { handleHttpError: 'fail' }
    })
  ],
  server: { fs: { allow: ['..'] } } // the docs live in ../docs
});
