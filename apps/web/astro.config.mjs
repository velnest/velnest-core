import { defineConfig } from 'astro/config';

// Static output; Vercel serves ./dist directly, so no adapter is needed.
export default defineConfig({
  output: 'static',
  redirects: {
    '/': '/id/',
  },
  i18n: {
    defaultLocale: 'id',
    locales: ['id', 'en'],
    routing: {
      prefixDefaultLocale: true,
      redirectToDefaultLocale: false,
    },
  },
});
