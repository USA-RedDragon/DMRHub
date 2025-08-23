import { routes } from '../src/router/routes.mjs';
import { Path } from 'path-parser';
import fs from 'fs';
import { SitemapStream, streamToPromise } from 'sitemap';
import { Readable } from 'stream';
import process from 'process';

console.log('Generating sitemap');

const stream = new SitemapStream({ hostname: 'https://dmrhub.net' });

const sitemapRoutes = [];

for (const route of routes({
  OpenBridge: '',
  isEnabled: () => {
    return false;
  },
})) {
  const path = new Path(route.path);

  if (!path.hasUrlParams) {
    if (!route.path.startsWith('/admin')) {
      sitemapRoutes.push({
        url: route.path,
        changefreq: route.sitemap.changefreq,
        priority: route.sitemap.priority,
      });
    }
  } else {
    console.error('You have URL parameters not accounted for in the sitemap');
    process.exit(1);
  }
}

streamToPromise(Readable.from(sitemapRoutes).pipe(stream)).then((data) => {
  fs.writeFileSync('public/sitemap.xml', data);
  console.log('Sitemap done');
});
