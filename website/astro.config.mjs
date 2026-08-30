import { defineConfig } from 'astro/config';
import tailwind from '@astrojs/tailwind';

const site = process.env.ASTRO_SITE || 'https://ngominhbinh708.github.io';
const base = process.env.ASTRO_BASE || (process.env.CI && process.env.GITHUB_REPOSITORY ? `/${process.env.GITHUB_REPOSITORY.split('/')[1]}` : '/');

// https://astro.build/config
export default defineConfig({
  integrations: [tailwind()],
  site,
  base,
  build: {
    format: 'directory'
  }
});
