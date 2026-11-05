// Call video arrives as numbered JPEG frames at /frame/<key>?n=<seq>.

// setFrame paints a frame into a canvas. The bytes are fetched and decoded off
// the main thread, and while one frame loads, newer ones only replace the URL
// waiting next: a slow moment skips frames instead of queueing them, so the
// picture never lags behind.
const want = new WeakMap<HTMLCanvasElement, string>();
const busy = new WeakSet<HTMLCanvasElement>();

export function setFrame(cv: HTMLCanvasElement, src: string) {
  want.set(cv, src);
  if (busy.has(cv)) return;
  busy.add(cv);
  const load = (url: string): Promise<void> => fetch(url)
    .then((r) => (r.ok ? r.blob() : null))
    .then((b) => b && createImageBitmap(b))
    .then((bm) => {
      if (!bm) return;
      if (cv.width !== bm.width || cv.height !== bm.height) { cv.width = bm.width; cv.height = bm.height; }
      cv.getContext("2d")!.drawImage(bm, 0, 0);
      bm.close();
    })
    .catch(() => {})
    .then(() => { const next = want.get(cv)!; if (next !== url) return load(next); busy.delete(cv); });
  load(src);
}

// WebKitGTK's media player cannot read from the wails:// scheme (it fails
// with MEDIA_ERR_SRC_NOT_SUPPORTED), but plays blob: URLs fine.
// ponytail: object URLs are kept for the session, one per media file opened.
const mediaURLs = new Map<string, Promise<string>>();
export function mediaURL(blob: string): Promise<string> {
  let p = mediaURLs.get(blob);
  if (!p) {
    p = fetch("/blob/" + blob).then((r) => {
      if (!r.ok) throw new Error(String(r.status));
      return r.blob();
    }).then((b) => URL.createObjectURL(b));
    p.catch(() => mediaURLs.delete(blob));
    mediaURLs.set(blob, p);
  }
  return p;
}
