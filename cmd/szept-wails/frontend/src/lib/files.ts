// File plumbing between the page and Go: data URLs go over the JSON bridge.

export function readAsDataURL(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as string);
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

export function dataURLFile(url: string, name: string): File {
  const i = url.indexOf(",");
  const bin = atob(url.slice(i + 1));
  const buf = new Uint8Array(bin.length);
  for (let j = 0; j < bin.length; j++) buf[j] = bin.charCodeAt(j);
  return new File([buf], name, { type: url.slice(5, url.indexOf(";")) });
}

// pickFiles opens the system chooser. The input is attached to the page:
// WebKit drops the change event of a detached input.
export function pickFiles(opts: { accept?: string; multiple?: boolean } = {}): Promise<File[]> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = !!opts.multiple;
    if (opts.accept) input.accept = opts.accept;
    input.hidden = true;
    document.body.append(input);
    input.onchange = () => {
      resolve([...(input.files || [])]);
      input.remove();
    };
    input.click();
  });
}

export async function copyText(t: string): Promise<boolean> {
  try { await navigator.clipboard.writeText(t); return true; } catch { return false; }
}
