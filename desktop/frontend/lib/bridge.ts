// Typed wrappers over the Wails bindings. The bindings live on window.go and
// window.runtime, which exist only inside the webview, so every access
// happens lazily inside a call, never at module scope: the static prerender
// runs in Node.

import type { AttachResult, SettingsPatch, SettingsView, UpdateInfo, View } from "./types"

type AppBindings = Record<string, (...args: unknown[]) => Promise<unknown>>

interface WailsWindow {
  go?: { bridge?: { App?: AppBindings } }
  runtime?: {
    EventsOn: (name: string, callback: (...data: unknown[]) => void) => () => void
    OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void
    OnFileDropOff?: () => void
  }
}

function app(): AppBindings {
  const bound = (globalThis as unknown as WailsWindow).go?.bridge?.App
  if (!bound) throw new Error("hrdx bridge unavailable")
  return bound
}

function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const fn = app()[method]
  if (!fn) return Promise.reject(new Error(`unknown method ${method}`))
  return fn(...args) as Promise<T>
}

/** Fire-and-forget call; errors are dropped on the hot path. */
function send(method: string, ...args: unknown[]): void {
  try {
    void call(method, ...args).catch(() => undefined)
  } catch {
    // bridge not ready
  }
}

export function available(): boolean {
  return Boolean((globalThis as unknown as WailsWindow).go?.bridge?.App)
}

export function on<T>(event: string, callback: (data: T) => void): () => void {
  const runtime = (globalThis as unknown as WailsWindow).runtime
  if (!runtime) return () => undefined
  return runtime.EventsOn(event, (data) => callback(data as T))
}

/** Subscribes to native file drops with window coordinates and absolute paths. */
export function onFileDrop(callback: (x: number, y: number, paths: string[]) => void): () => void {
  const runtime = (globalThis as unknown as WailsWindow).runtime
  if (!runtime?.OnFileDrop) return () => undefined
  runtime.OnFileDrop(callback, false)
  return () => runtime.OnFileDropOff?.()
}

export const bridge = {
  snapshot: () => call<View>("Snapshot"),
  version: () => call<string>("Version"),

  workspaceCreate: (path: string, agent: string, groupPath: string[] = []) =>
    call<void>("WorkspaceCreate", path, agent, groupPath),
  workspacePickDirectory: () => call<string>("WorkspacePickDirectory"),
  workspaceSelect: (index: number) => call<void>("WorkspaceSelect", index),
  workspaceClose: (index: number) => call<void>("WorkspaceClose", index),
  workspaceReorder: (from: number, to: number) => call<void>("WorkspaceReorder", from, to),
  workspaceMove: (index: number, groupPath: string[]) => call<void>("WorkspaceMove", index, groupPath),
  subrepoOpen: (workspace: number, path: string, kind: string, where: "tab" | "right" | "down") =>
    call<void>("SubrepoOpen", workspace, path, kind, where),
  subrepoHide: (workspace: number, path: string) => call<void>("SubrepoHide", workspace, path),
  subrepoUnhide: (workspace: number) => call<void>("SubrepoUnhide", workspace),
  workspaceSetFolded: (index: number, folded: boolean) => call<void>("WorkspaceSetFolded", index, folded),
  workspaceRename: (index: number, name: string) => call<void>("WorkspaceRename", index, name),
  completePath: (prefix: string) => call<string[]>("CompletePath", prefix),
  find: (query: string) => call<{ label: string; pane_id: number }[]>("Find", query),

  tabCreate: (kind: string) => call<void>("TabCreate", kind),
  tabSelect: (workspace: number, tab: number) => call<void>("TabSelect", workspace, tab),
  tabClose: (workspace: number, tab: number) => call<void>("TabClose", workspace, tab),
  tabRename: (workspace: number, tab: number, name: string) => call<void>("TabRename", workspace, tab, name),

  paneCreate: (kind: string, split: string) => call<void>("PaneCreate", kind, split),
  paneCreateFloat: (kind: string, anchor: string, width: number, height: number) =>
    call<void>("PaneCreateFloat", kind, anchor, width, height),
  paneFocus: (id: number) => call<void>("PaneFocus", id),
  paneClose: (id: number) => call<void>("PaneClose", id),
  paneMove: (id: number, target: number, zone: string) => call<void>("PaneMove", id, target, zone),
  paneRename: (id: number, name: string) => call<void>("PaneRename", id, name),
  paneRestart: (id: number) => call<void>("PaneRestart", id),
  paneAttach: (id: number) => call<AttachResult>("PaneAttach", id),
  paneInput: (id: number, data: string) => send("PaneInput", id, data),
  paneInputBinary: (id: number, base64: string) => send("PaneInputBinary", id, base64),
  paneResize: (id: number, cols: number, rows: number) => send("PaneResize", id, cols, rows),

  splitSetRatio: (path: number[], ratio: number) => call<void>("SplitSetRatio", path, ratio),
  splitEqualize: (path: number[]) => call<void>("SplitEqualize", path),
  splitDirectionCycle: () => call<void>("SplitDirectionCycle"),
  navigate: (action: string) => call<void>("Navigate", action),
  menuAction: (actionID: string, target: string, paneID: number, workspace: number, tab: number) =>
    call<void>("MenuAction", actionID, target, paneID, workspace, tab),

  settingsGet: () => call<SettingsView>("SettingsGet"),
  settingsSet: (patch: SettingsPatch) => call<void>("SettingsSet", patch),
  soundPreview: (name: string) => call<void>("SoundPreview", name),

  clipboardWrite: (text: string) => call<void>("ClipboardWrite", text),
  clipboardRead: () => call<string>("ClipboardRead"),
  openExternal: (url: string) => call<void>("OpenExternal", url),
  windowFocus: (focused: boolean) => send("WindowFocus", focused),
  dismissProblems: () => send("DismissProblems"),
  retryHolder: () => call<void>("RetryHolder"),
  updateCheck: () => call<UpdateInfo>("UpdateCheck"),
  updateInstall: () => call<void>("UpdateInstall"),
  quit: () => call<void>("Quit"),
}

export function errorMessage(error: unknown): string {
  if (typeof error === "string") return error
  if (error instanceof Error) return error.message
  return String(error)
}
