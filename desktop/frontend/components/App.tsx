"use client"

import { useEffect, type CSSProperties, type MouseEvent } from "react"
import { available, bridge, on, onFileDrop } from "@/lib/bridge"
import { quotePaths } from "@/lib/xterm"
import {
  confirmCloseWorkspace,
  floatPicker,
  moveWorkspace,
  openPicker,
  renameTab,
  renameWorkspace,
  report,
  splitPicker,
} from "@/lib/actions"
import { activeTab, getView, selectedWorkspace, setView, tabLabel, useView, workspaces } from "@/lib/store"
import { cssVariables } from "@/lib/theme"
import type { MenuRegistration, PaneView, SubrepoView, View } from "@/lib/types"
import { closeModal, openMenu, openModal, terminalByID, toast, useUI, type MenuItem } from "@/lib/ui"
import ContextMenu from "./ContextMenu"
import KeyRouter, { configureKeys } from "./KeyRouter"
import Modals from "./Modals"
import Sidebar from "./Sidebar"
import ReviewPanel from "./ReviewPanel"
import SplitView from "./SplitView"
import TabBar from "./TabBar"
import UpdateNotice from "./UpdateNotice"
import styles from "./App.module.css"

/** Custom entries from menu.register for a target, with their action. */
function customItems(view: View, target: MenuRegistration["target"], context: { pane?: number; workspace: number; tab?: number }): MenuItem[] {
  return (view.menus ?? [])
    .filter((menu) => menu.target === target)
    .map((menu) => ({
      label: menu.label,
      run: () => report(bridge.menuAction(menu.action_id, target, context.pane ?? 0, context.workspace, context.tab ?? -1)),
    }))
}

function workspaceIndexOfPane(view: View, paneID: number): { workspace: number; tab: number } | undefined {
  for (const [workspace, ws] of workspaces(view).entries()) {
    for (const [tab, current] of (ws.tabs ?? []).entries()) {
      if ([...(current.panes ?? []), ...(current.floats ?? [])].some((pane) => pane.pane_id === paneID)) return { workspace, tab }
    }
  }
  return undefined
}

function paneMenu(event: { clientX: number; clientY: number; preventDefault?: () => void }, pane: PaneView) {
  event.preventDefault?.()
  const view = getView()
  if (!view) return
  const where = workspaceIndexOfPane(view, pane.pane_id)
  const items: MenuItem[] = [
    { label: "Split right", run: () => report(bridge.paneFocus(pane.pane_id).then(() => splitPicker("right"))) },
    { label: "Split down", run: () => report(bridge.paneFocus(pane.pane_id).then(() => splitPicker("down"))) },
    { label: "Floating pane", run: floatPicker },
    {
      label: "Rename",
      run: () =>
        openModal({ kind: "rename", title: "Rename pane", initial: pane.name, onSubmit: (name) => bridge.paneRename(pane.pane_id, name) }),
    },
  ]
  if (!pane.running && !pane.starting) items.push({ label: "Restart", run: () => report(bridge.paneRestart(pane.pane_id)) })
  items.push(...customItems(view, "pane", { pane: pane.pane_id, workspace: where?.workspace ?? 0, tab: where?.tab }))
  items.push({ label: "Close pane", danger: true, run: () => report(bridge.paneClose(pane.pane_id)) })
  openMenu({ x: event.clientX, y: event.clientY, items })
}

function tabMenu(event: MouseEvent, workspace: number, tab: number) {
  event.preventDefault()
  const view = getView()
  const ws = workspaces(view)[workspace]
  const current = ws?.tabs?.[tab]
  if (!view || !ws || !current) return
  const items: MenuItem[] = [
    { label: "Rename tab", run: () => renameTab(workspace, tab, current.name ?? "") },
    { label: "New tab", run: () => report(bridge.tabSelect(workspace, tab).then(() => splitPicker("tab"))) },
    ...customItems(view, "tab", { workspace, tab }),
  ]
  if ((ws.tabs ?? []).length > 1) items.push({ label: `Close ${tabLabel(current, tab)}`, danger: true, run: () => report(bridge.tabClose(workspace, tab)) })
  openMenu({ x: event.clientX, y: event.clientY, items })
}

function workspaceMenu(event: MouseEvent, index: number) {
  event.preventDefault()
  const view = getView()
  const ws = workspaces(view)[index]
  if (!view || !ws) return
  const items: MenuItem[] = [
    { label: "New tab", run: () => report(bridge.workspaceSelect(index).then(() => splitPicker("tab"))) },
    { label: "Rename workspace", run: () => renameWorkspace(index, ws.name) },
    { label: "Move to group", run: () => moveWorkspace(index, ws.group_path ?? []) },
  ]
  if (view.workspace_folding) items.push({ label: ws.folded ? "Unfold" : "Fold", run: () => report(bridge.workspaceSetFolded(index, !ws.folded)) })
  if (ws.hidden_subrepos) {
    items.push({ label: `Show hidden folders (${ws.hidden_subrepos})`, run: () => report(bridge.subrepoUnhide(index)) })
  }
  items.push(...customItems(view, "sidebar", { workspace: index }))
  items.push({ label: "Close workspace", danger: true, run: () => confirmCloseWorkspace(index) })
  openMenu({ x: event.clientX, y: event.clientY, items })
}

/** Where to open a nested repository, like the TUI's sub-repository picker. */
function subrepoMenu(event: MouseEvent, workspace: number, repo: SubrepoView) {
  event.preventDefault()
  const open = (title: string, where: "tab" | "right" | "down") => () =>
    openPicker(title, (kind) => {
      closeModal()
      report(bridge.subrepoOpen(workspace, repo.path, kind, where))
    })
  const items: MenuItem[] = [
    { label: "New tab here", run: open(`New tab in ${repo.rel}`, "tab") },
    { label: "Split right here", run: open(`Split right in ${repo.rel}`, "right") },
    { label: "Split down here", run: open(`Split down in ${repo.rel}`, "down") },
    {
      label: "Open as workspace",
      run: () =>
        openPicker(`Open ${repo.rel} as workspace`, (kind) => {
          closeModal()
          report(bridge.workspaceCreate(repo.path, kind))
        }),
    },
    { label: "Hide folder", danger: true, run: () => report(bridge.subrepoHide(workspace, repo.path)) },
  ]
  openMenu({ x: event.clientX, y: event.clientY, items })
}

function PrefixHint({ view }: { view: View }) {
  const entries: [string, string][] = [
    ["picker-right", "pane"],
    ["tab-new", "tab"],
    ["workspace", "workspace"],
    ["find", "find"],
    ["files", "files"],
    ["changes", "changes"],
    ["pane-next", "next pane"],
    ["rename", "rename"],
    ["close-pane", "close"],
    ["settings", "settings"],
    ["quit", "quit"],
  ]
  const keyFor = (action: string) => Object.entries(view.keymap).find(([, value]) => value === action)?.[0]
  return (
    <div className={styles.prefix}>
      <span className={styles.badge}>{view.prefix}</span>
      <div className={styles.prefixItems}>
        {entries.map(([action, label]) => {
          const key = keyFor(action)
          return key ? (
            <span key={action} className={styles.hintItem}>
              <kbd>{key}</kbd>
              {label}
            </span>
          ) : null
        })}
        <span className={styles.hintItem}>
          <kbd>esc</kbd>
          cancel
        </span>
      </div>
    </div>
  )
}

function EmptyState() {
  return (
    <div className={styles.empty}>
      <h1>hrdx</h1>
      <p>Open a project directory as a workspace to start agents and shells.</p>
      <button onClick={() => openModal({ kind: "workspace" })}>New workspace</button>
    </div>
  )
}

export default function App() {
  const view = useView()
  const ui = useUI()

  useEffect(() => {
    if (!available()) return
    const offState = on<View>("state", (next) => {
      setView(next)
      configureKeys(next.prefix, next.keymap)
    })
    const offMenu = (() => {
      const handler = (event: Event) => {
        const id = (event as CustomEvent<number>).detail
        const pane = activeTab(selectedWorkspace(getView()))
        const found = [...(pane?.panes ?? []), ...(pane?.floats ?? [])].find((candidate) => candidate.pane_id === id)
        if (found) paneMenu({ clientX: window.innerWidth / 2 - 90, clientY: 80 }, found)
      }
      document.addEventListener("hrdx:pane-menu", handler)
      return () => document.removeEventListener("hrdx:pane-menu", handler)
    })()
    // Dropped files type their quoted paths into the pane under the cursor,
    // like a terminal, bracketed for agents as in the TUI.
    const offDrop = onFileDrop((x, y, paths) => {
      if (paths.length === 0) return
      const frame = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-pane-id]")
      const id = Number(frame?.dataset.paneId)
      if (!id) return
      void bridge.paneFocus(id).catch(() => undefined)
      terminalByID(id)?.paste(quotePaths(paths))
    })
    void bridge
      .snapshot()
      .then((initial) => {
        setView(initial)
        configureKeys(initial.prefix, initial.keymap)
      })
      .catch((error: unknown) => toast(String(error)))
    return () => {
      offState()
      offMenu()
      offDrop()
    }
  }, [])

  // The whole document follows the theme so modals match the chrome.
  const themeVars = cssVariables(view?.theme.colors ?? {})
  useEffect(() => {
    for (const [name, value] of Object.entries(themeVars)) document.documentElement.style.setProperty(name, value)
  })

  if (!view) {
    return (
      <div className={styles.loading} style={themeVars as CSSProperties}>
        hrdx
        <Modals />
      </div>
    )
  }

  const workspace = selectedWorkspace(view)
  const tab = activeTab(workspace)
  const problems = view.problems ?? []

  return (
    <div className={styles.app} style={themeVars as CSSProperties}>
      <KeyRouter />
      <Sidebar view={view} onWorkspaceMenu={workspaceMenu} onTabMenu={tabMenu} onPaneMenu={paneMenu} onSubrepoMenu={subrepoMenu} />
      <main className={styles.main}>
        {workspace && tab ? (
          <TabBar workspace={workspace} index={view.selected} onTabMenu={tabMenu} />
        ) : (
          <div className={styles.titlebar} />
        )}
        <UpdateNotice />
        {problems.length > 0 ? (
          <div className={styles.banner}>
            <div>
              {problems.map((problem, index) => (
                <div key={index}>{problem}</div>
              ))}
            </div>
            <button onClick={() => bridge.dismissProblems()}>Dismiss</button>
          </div>
        ) : null}
        {workspace && tab ? (
          <>
            {/* Only the active tab mounts terminals; inactive ones remount from the replay ring. */}
            <div className={styles.work}>
              <SplitView key={`${view.selected}:${workspace.active}`} tab={tab} path={workspace.path} onPaneMenu={paneMenu} />
              {ui.panel ? <ReviewPanel panel={ui.panel} /> : null}
            </div>
          </>
        ) : (
          <EmptyState />
        )}
        {ui.prefix ? <PrefixHint view={view} /> : null}
        {!view.persisting ? <div className={styles.persistNote}>Session holder unavailable: panes end when the app quits.</div> : null}
      </main>
      {ui.toast ? <div className={styles.toast}>{ui.toast}</div> : null}
      <ContextMenu />
      <Modals />
    </div>
  )
}
