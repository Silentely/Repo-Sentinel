// RepoSentinel PWA Service Worker
const CACHE_NAME = 'reposentinel-v3';
const PRECACHE_ASSETS = [
  '/',
  '/favicon.svg',
  '/favicon.ico',
  '/apple-touch-icon.png',
  '/manifest.webmanifest',
  '/theme-init.js',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE_ASSETS)).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.map((key) => {
          if (key !== CACHE_NAME) {
            return caches.delete(key);
          }
        })
      )
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // 绝不拦截 API 接口、实时 SSE 推流以及外部跨域请求
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/events')) {
    return;
  }

  // 导航请求（HTML 页面）：网络优先，离线回退到应用外壳缓存
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request).catch(async () => {
        const cache = await caches.open(CACHE_NAME);
        const cached = await cache.match('/');
        if (cached) {
          return cached;
        }
        return new Response('<h1>RepoSentinel 离线中</h1><p>网络连接暂时断开，请检查网络后重试。</p>', {
          headers: { 'Content-Type': 'text/html; charset=utf-8' },
        });
      })
    );
    return;
  }

  // 非哈希的预存资源（theme-init.js、manifest、图标）：路径固定，必须网络优先并回写缓存。
  // 走缓存优先会让部署后的旧内容一直驻留到手动改 CACHE_NAME。
  if (PRECACHE_ASSETS.includes(url.pathname)) {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response.ok) {
            const copy = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return response;
        })
        .catch(async () => {
          const cached = await caches.match(event.request);
          if (cached) {
            return cached;
          }
          return new Response('', { status: 504, statusText: 'Offline' });
        }),
    );
    return;
  }

  // Vite 产出的 hash 静态资源在首次在线访问时缓存；离线导航返回缓存的
  // index.html 后，入口 JS/CSS 仍可加载，避免只显示空 root 节点。
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(
      caches.match(event.request).then(async (cached) => {
        if (cached) return cached;
        const response = await fetch(event.request);
        if (response.ok) {
          const cache = await caches.open(CACHE_NAME);
          await cache.put(event.request, response.clone());
        }
        return response;
      }),
    );
  }
});
