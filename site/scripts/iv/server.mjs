// Static-file HTTP server over `dist/` for local IV runs. We can't trust
// `astro preview` to be installed in CI; rolling a tiny server keeps the
// IV script self-contained and avoids racing astro's startup output.
//
// Single port, sync mime mapping, no caching. Serves `index.html` for
// directory paths so Astro's pretty URLs (`/docs/`, `/docs/concepts/`)
// resolve correctly.

import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { join, resolve, normalize } from 'node:path';

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.xml': 'application/xml; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.pf_meta': 'application/octet-stream',
  '.pf_index': 'application/octet-stream',
  '.pf_fragment': 'application/octet-stream',
};

function mimeFor(path) {
  const ext = path.slice(path.lastIndexOf('.')).toLowerCase();
  return MIME[ext] || 'application/octet-stream';
}

export async function startServer(rootDir, port = 0) {
  const root = resolve(rootDir);

  const server = createServer(async (req, res) => {
    try {
      const urlPath = decodeURIComponent(new URL(req.url, 'http://x').pathname);
      // Prevent path traversal — normalize, then ensure resolved path is
      // inside `root`. Astro's static output never legitimately escapes.
      const candidate = normalize(join(root, urlPath));
      if (!candidate.startsWith(root)) {
        res.writeHead(403);
        res.end('forbidden');
        return;
      }

      let filePath = candidate;
      let stats;
      try {
        stats = await stat(filePath);
      } catch {
        // 404 — serve the custom 404 page if present so test harnesses see
        // the real C-3PO 404 markup (R-U-6) instead of a default body.
        const fallback = join(root, '404.html');
        try {
          const body = await readFile(fallback);
          res.writeHead(404, { 'content-type': 'text/html; charset=utf-8' });
          res.end(body);
          return;
        } catch {
          res.writeHead(404);
          res.end('not found');
          return;
        }
      }

      if (stats.isDirectory()) {
        filePath = join(filePath, 'index.html');
        try {
          stats = await stat(filePath);
        } catch {
          res.writeHead(404);
          res.end('not found');
          return;
        }
      }

      const body = await readFile(filePath);
      res.writeHead(200, { 'content-type': mimeFor(filePath) });
      res.end(body);
    } catch (err) {
      res.writeHead(500);
      res.end(`error: ${err.message}`);
    }
  });

  await new Promise((r) => server.listen(port, '127.0.0.1', r));
  const address = server.address();
  const actualPort = typeof address === 'object' && address ? address.port : port;
  const baseUrl = `http://127.0.0.1:${actualPort}`;

  return {
    baseUrl,
    port: actualPort,
    async stop() {
      await new Promise((r) => server.close(r));
    },
  };
}
