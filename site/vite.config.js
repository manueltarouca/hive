import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [
    sveltekit({
      adapter: adapter({ fallback: '404.html' }),
      // GitHub Pages serves the project at /hive.
      paths: { base: process.env.BASE_PATH ?? '' },
      prerender: { handleHttpError: 'fail' }
    })
  ],
  server: { fs: { allow: ['..'] } } // the docs live in ../docs
});
