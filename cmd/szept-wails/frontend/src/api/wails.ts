// The bridge to Go. @wailsio/runtime is pinned in package.json to the version
// bundled with the wails Go module in go.mod; bump both together.
import { Call, Events } from "@wailsio/runtime";

// api calls a method of the App service (api.go) by name.
export function api<T = unknown>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName("main.App." + method, ...args) as Promise<T>;
}

// onEvent subscribes to an event Go emits; the callback gets its payload.
export function onEvent<T = unknown>(name: string, cb: (data: T) => void): () => void {
  return Events.On(name, (e: { data: unknown }) => cb(e.data as T));
}
