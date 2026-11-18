// Prefix actions and native shortcuts, shared by the key handler and menus.

import { bridge, errorMessage } from "./bridge"
import { activeTab, focusedPane, getView, selectedWorkspace, workspaces } from "./store"
import { closeModal, focusedTerminal, openModal, toast, togglePanel } from "./ui"

/** Reports a rejected bridge call as a toast. */
export function report(promise: Promise<unknown>): void {
  promise.catch((error: unknown) => toast(errorMessage(error)))
}

/** Lists agents for pickers: installed and enabled, then shell. */
export function pickerKinds(): string[] {
  const view = getView()
  const agents = (view?.agents ?? []).filter((agent) => agent.enabled)
  const installed = agents.filter((agent) => agent.installed).map((agent) => agent.kind)
  const kinds = installed.length > 0 ? installed : agents.map((agent) => agent.kind)
  return [...kinds, "shell"]
}

export function openPicker(title: string, onPick: (kind: string) => void): void {
  openModal({ kind: "picker", title, onPick })
}

export function splitPicker(split: string): void {
  openPicker(split === "tab" ? "New tab" : "New pane", (kind) => {
    closeModal()
    report(split === "tab" ? bridge.tabCreate(kind) : bridge.paneCreate(kind, split))
  })
}

export function floatPicker(): void {
  openPicker("Floating pane", (kind) => {
    closeModal()
    report(bridge.paneCreateFloat(kind, "center", 70, 70))
  })
}

export function renameFocusedPane(): void {
  const pane = focusedPane(getView())
  if (!pane) return
  openModal({
    kind: "rename",
    title: "Rename pane",
    initial: pane.name,
    onSubmit: (name) => bridge.paneRename(pane.pane_id, name),
  })
}

export function renameTab(workspace: number, tab: number, initial: string): void {
  openModal({
    kind: "rename",
    title: "Rename tab",
    initial,
    allowEmpty: true,
    onSubmit: (name) => bridge.tabRename(workspace, tab, name),
  })
}

export function renameWorkspace(workspace: number, initial: string): void {
  openModal({
    kind: "rename",
    title: "Rename workspace",
    initial,
    onSubmit: (name) => bridge.workspaceRename(workspace, name),
  })
}

export function moveWorkspace(workspace: number, initial: string[]): void {
  openModal({
    kind: "rename",
    title: "Group (use / between levels, empty for none)",
    initial: initial.join(" / "),
    allowEmpty: true,
    onSubmit: (value) => {
      const path = value
        .split("/")
        .map((part) => part.trim())
        .filter((part) => part !== "")
      return bridge.workspaceMove(workspace, path)
    },
  })
}

export function confirmCloseWorkspace(index: number): void {
  const workspace = workspaces(getView())[index]
  if (!workspace) return
  openModal({
    kind: "confirm",
    title: "Close workspace",
    message: `Close ${workspace.name} and kill all of its panes?`,
    action: "Close",
    onConfirm: () => bridge.workspaceClose(index),
  })
}

export function quit(): void {
  report(bridge.quit())
}

/** Runs one prefix action. Returns false when the action is unknown. */
export function runAction(action: string): boolean {
  const view = getView()
  const vertical = (primary: boolean) => (primary ? "primary" : "secondary")
  switch (action) {
    case "literal": {
      const pane = focusedPane(view)
      if (pane) bridge.paneInput(pane.pane_id, "\x02")
      return true
    }
    case "quit":
      quit()
      return true
    case "picker-right":
    case "picker-down":
      splitPicker(vertical(action === "picker-right"))
      return true
    case "agent-right":
    case "agent-down":
      report(bridge.paneCreate(view?.default_agent ?? "zot", vertical(action === "agent-right")))
      return true
    case "shell-right":
    case "shell-down":
      report(bridge.paneCreate("shell", vertical(action === "shell-right")))
      return true
    case "split-cycle":
      report(bridge.splitDirectionCycle())
      return true
    case "agent-cycle":
      report(bridge.navigate("agent-cycle"))
      return true
    case "workspace":
      openModal({ kind: "workspace" })
      return true
    case "tab-new":
      splitPicker("tab")
      return true
    case "find":
      openModal({ kind: "finder" })
      return true
    case "rename":
      renameFocusedPane()
      return true
    case "settings":
      openModal({ kind: "settings" })
      return true
    case "files":
      togglePanel("files")
      return true
    case "changes":
      togglePanel("changes")
      return true
    case "menu": {
      const pane = focusedPane(view)
      if (pane) document.dispatchEvent(new CustomEvent("hrdx:pane-menu", { detail: pane.pane_id }))
      return true
    }
    case "close-space":
      if (view) confirmCloseWorkspace(view.selected)
      return true
    case "scroll-up":
      focusedTerminal()?.scrollPages(-0.5)
      return true
    case "scroll-down":
      focusedTerminal()?.scrollPages(0.5)
      return true
    case "live":
      focusedTerminal()?.scrollToBottom()
      return true
    case "continuous":
    case "durable-right":
    case "plugins":
      toast("Not available in the hrdx app yet")
      return true
    case "tab-next":
    case "tab-prev":
    case "tab-move-left":
    case "tab-move-right":
    case "space-next":
    case "space-prev":
    case "pane-next":
    case "pane-prev":
    case "sidebar-toggle":
    case "close-pane":
    case "equalize":
      report(bridge.navigate(action))
      return true
  }
  if (/^tab-\d$/.test(action)) {
    report(bridge.navigate(action))
    return true
  }
  return false
}

/** Selected workspace and tab indexes for menus. */
export function currentIndexes(): { workspace: number; tab: number } {
  const view = getView()
  const workspace = selectedWorkspace(view)
  return { workspace: view?.selected ?? 0, tab: workspace?.active ?? 0 }
}

export { activeTab }
