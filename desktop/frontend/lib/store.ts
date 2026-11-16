// Latest-wins snapshot store. Components subscribe with
// useSyncExternalStore; terminal output never passes through here.

import { useSyncExternalStore } from "react"
import type { PaneView, TabView, View, WorkspaceView } from "./types"

type Listener = () => void

let current: View | null = null
const listeners = new Set<Listener>()

export function setView(view: View): void {
  current = view
  for (const listener of listeners) listener()
}

export function getView(): View | null {
  return current
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useView(): View | null {
  return useSyncExternalStore(subscribe, getView, () => null)
}

export function workspaces(view: View | null): WorkspaceView[] {
  return view?.workspaces ?? []
}

export function selectedWorkspace(view: View | null): WorkspaceView | undefined {
  if (!view) return undefined
  return workspaces(view)[view.selected]
}

export function activeTab(workspace: WorkspaceView | undefined): TabView | undefined {
  if (!workspace) return undefined
  return (workspace.tabs ?? [])[workspace.active]
}

export function tabPanes(tab: TabView | undefined): PaneView[] {
  if (!tab) return []
  return [...(tab.panes ?? []), ...(tab.floats ?? [])]
}

export function focusedPane(view: View | null): PaneView | undefined {
  const tab = activeTab(selectedWorkspace(view))
  if (!tab) return undefined
  return tabPanes(tab).find((pane) => pane.pane_id === tab.focused_pane_id)
}

export function tabLabel(tab: TabView, index: number): string {
  if (tab.name) return tab.name
  const first = tab.panes?.[0]
  return first ? `${index + 1} ${first.agent || first.kind}` : `${index + 1}`
}
