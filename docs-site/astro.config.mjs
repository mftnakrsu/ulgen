// @ts-check
import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';

// The product introduction and documentation at https://ulgen.mefby.com, published to GitHub Pages
// with a custom domain (set in the repository's Pages settings, not with a CNAME file, because the
// site deploys through GitHub Actions). The site lives at the root, so internal links start with `/`.
//
// The pages keep the paths the MkDocs site had at https://mftnakrsu.github.io/ulgen/ (/install/,
// /using/, /evren/, /tr/evren-ile-kodlama-araclari/): GitHub sends the old addresses to the same
// path on the custom domain.
export default defineConfig({
  site: 'https://ulgen.mefby.com',
  integrations: [
    starlight({
      title: 'Ülgen',
      description:
        "Ülgen connects EVREN, the model API run by Türkiye's Presidency of Defence Industries (SSB), to Qwen Code, opencode, Codex CLI and Claude Code.",
      logo: { src: './src/assets/logo.svg' },
      favicon: '/favicon.svg',
      head: [
        {
          // The demo video autoplays. For people who ask for reduced motion it stays paused and
          // gets controls, so they can still start it.
          tag: 'script',
          content:
            "document.addEventListener('DOMContentLoaded',()=>{if(!matchMedia('(prefers-reduced-motion: reduce)').matches)return;for(const v of document.querySelectorAll('video[autoplay]')){v.autoplay=false;v.pause();v.controls=true;}});",
        },
      ],
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/mftnakrsu/ulgen' }],
      editLink: { baseUrl: 'https://github.com/mftnakrsu/ulgen/edit/main/docs-site/' },
      locales: {
        root: { label: 'English', lang: 'en' },
        tr: { label: 'Türkçe', lang: 'tr' },
      },
      customCss: ['./src/styles/custom.css'],
      components: { Footer: './src/components/Footer.astro' },
      // src/pages/404.astro renders the 404 page instead.
      disable404Route: true,
      sidebar: [
        'install',
        'using',
        // The long guide is in Turkish only, as on the MkDocs site; the English page at this slug
        // says so and links to it.
        'evren-ile-kodlama-araclari',
        'evren',
      ],
    }),
  ],
});
