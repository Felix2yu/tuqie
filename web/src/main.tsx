import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './styles.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

// Only the built app is installed as a service worker: under the dev server the
// cached shell would hide every change being made.
if (import.meta.env.PROD) {
  window.addEventListener('load', () => {
    // Ask for the API by name first: a browser outside a secure context does not
    // expose it at all, so testing the context and then the API would leave the
    // common case — the plain-http address a container publishes on a LAN —
    // failing in silence. Without a worker there is no install and no offline,
    // and nothing on screen says which of the two the visitor is missing.
    if (!('serviceWorker' in navigator)) {
      console.warn('图切：这个页面拿不到 service worker（不是 HTTPS 也不是 localhost，无痕窗口同样会关掉），所以装不到主屏幕，也没有离线。');
      return;
    }
    navigator.serviceWorker.register('/sw.js').catch((err) => {
      console.warn('图切：service worker 注册失败，安装与离线不可用。', err);
    });
  });
}
