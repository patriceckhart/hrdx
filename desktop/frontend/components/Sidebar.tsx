"use client"

import { useRef, useState, type MouseEvent, type PointerEvent } from "react"
import { bridge } from "@/lib/bridge"
import { report } from "@/lib/actions"
import { tabLabel } from "@/lib/store"
import type { PaneView, SubrepoView, View, WorkspaceView } from "@/lib/types"
import { DRAG_THRESHOLD, reorderTarget } from "@/lib/reorder"
import { subrepoTree } from "@/lib/subrepos"
import { openModal } from "@/lib/ui"
import { AgentIcon, LineIcon, WorkspaceIcon } from "./Icons"
import { paneStatus } from "./PaneFrame"
import styles from "./Sidebar.module.css"

interface Props {
  view: View
  onWorkspaceMenu: (event: MouseEvent, index: number) => void
  onTabMenu: (event: MouseEvent, workspace: number, tab: number) => void
  onPaneMenu: (event: MouseEvent, pane: PaneView) => void
  onSubrepoMenu: (event: MouseEvent, workspace: number, repo: SubrepoView) => void
}

const COLLAPSED = 64

interface GroupNode {
  name: string
  children: GroupNode[]
  workspaces: number[]
}

/** Groups workspaces by group path, keeping first-workspace order. */
export function groupTree(workspaces: WorkspaceView[]): GroupNode {
  const root: GroupNode = { name: "", children: [], workspaces: [] }
  workspaces.forEach((workspace, index) => {
    let node = root
    for (const name of workspace.group_path ?? []) {
      let child = node.children.find((candidate) => candidate.name === name)
      if (!child) {
        child = { name, children: [], workspaces: [] }
        node.children.push(child)
      }
      node = child
    }
    node.workspaces.push(index)
  })
  return root
}

export default function Sidebar({ view, onWorkspaceMenu, onTabMenu, onPaneMenu, onSubrepoMenu }: Props) {
  const workspaces = view.workspaces ?? []
  const collapsed = view.sidebar_collapsed
  const settingsKey = typeof navigator !== "undefined" && /Mac/.test(navigator.platform) ? "⌘," : ""
  const [width, setWidth] = useState<number | null>(null)
  // Drop marker: the workspace under the pointer and which half it is over.
  const [dragOver, setDragOver] = useState<{ index: number; after: boolean } | null>(null)
  const [dragging, setDragging] = useState<number | null>(null)
  const suppressClick = useRef(false)

  // Workspaces reorder with pointer events, not HTML5 drag and drop: Wails
  // swallows native drops (DisableWebViewDrop) so `drop` never fires.
  const startReorder = (event: PointerEvent, from: number) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest("[data-no-drag]")) return
    const startX = event.clientX
    const startY = event.clientY
    let active = false
    let target: { index: number; after: boolean } | null = null
    const hit = (x: number, y: number) => {
      const element = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-ws-index]")
      if (!element) return null
      const rect = element.getBoundingClientRect()
      return { index: Number(element.dataset.wsIndex), after: y > rect.top + rect.height / 2 }
    }
    const move = (moveEvent: globalThis.PointerEvent) => {
      if (!active) {
        if (Math.hypot(moveEvent.clientX - startX, moveEvent.clientY - startY) < DRAG_THRESHOLD) return
        active = true
        setDragging(from)
      }
      target = hit(moveEvent.clientX, moveEvent.clientY)
      setDragOver(target)
    }
    const up = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
      window.removeEventListener("pointercancel", up)
      setDragging(null)
      setDragOver(null)
      if (!active) return
      // The release also fires a click on the row: don't select by accident.
      suppressClick.current = true
      setTimeout(() => (suppressClick.current = false), 0)
      const to = target ? reorderTarget(from, target.index, target.after) : null
      if (to !== null) report(bridge.workspaceReorder(from, to))
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
    window.addEventListener("pointercancel", up)
  }
  const shown = collapsed ? COLLAPSED : (width ?? view.sidebar_width)

  const resize = (event: PointerEvent) => {
    if (collapsed) return
    event.preventDefault()
    const startX = event.clientX
    const startW = shown
    let latest = startW
    const move = (moveEvent: globalThis.PointerEvent) => {
      latest = Math.min(800, Math.max(140, startW + moveEvent.clientX - startX))
      setWidth(latest)
    }
    const up = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
      report(bridge.settingsSet({ prefs: { ...view.prefs, sidebar_width: latest } }).finally(() => setWidth(null)))
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
  }

  const renderWorkspace = (index: number) => {
    const workspace = workspaces[index]!
    const selected = index === view.selected
    const tabs = workspace.tabs ?? []
    const folded = view.workspace_folding && workspace.folded
    const allPanes = tabs.flatMap((tab) => [...(tab.panes ?? []), ...(tab.floats ?? [])])
    const busy = allPanes.some((pane) => pane.busy)
    const unseen = allPanes.some((pane) => pane.unseen)
    const attention = allPanes.some((pane) => pane.attention)

    if (collapsed) {
      return (
        <button
          key={index}
          className={styles.compact}
          data-selected={selected || undefined}
          title={`${workspace.name}\n${workspace.path}`}
          onClick={() => report(bridge.workspaceSelect(index))}
          onContextMenu={(event) => onWorkspaceMenu(event, index)}
        >
          <WorkspaceIcon name={workspace.name} size={36} />
          {attention ? <i className={styles.badge} data-tone="attention" /> : busy ? <i className={styles.badge} data-tone="busy" /> : unseen ? <i className={styles.badge} data-tone="done" /> : null}
        </button>
      )
    }

    return (
      <div
        key={index}
        className={styles.workspace}
        data-selected={selected || undefined}
        data-ws-index={index}
        data-drop={dragOver?.index === index && dragging !== index ? (dragOver.after ? "after" : "before") : undefined}
        data-dragging={dragging === index || undefined}
      >
        <div
          className={styles.workspaceRow}
          title={workspace.path}
          onPointerDown={(event) => startReorder(event, index)}
          onClick={() => {
            if (!suppressClick.current) report(bridge.workspaceSelect(index))
          }}
          onContextMenu={(event) => onWorkspaceMenu(event, index)}
        >
          {view.workspace_folding ? (
            <span
              className={styles.fold}
              data-no-drag
              data-folded={workspace.folded || undefined}
              title={workspace.folded ? "Unfold" : "Fold"}
              onClick={(event) => {
                event.stopPropagation()
                report(bridge.workspaceSetFolded(index, !workspace.folded))
              }}
            >
              <LineIcon name="chevron" size={14} />
            </span>
          ) : null}
          <WorkspaceIcon name={workspace.name} />
          <span className={styles.workspaceName}>{workspace.name}</span>
          {workspace.branch ? <span className={styles.branch}>{workspace.branch}</span> : null}
          {attention ? (
            <span className={styles.pill}>Needs you</span>
          ) : busy ? (
            <i className={styles.spinner} />
          ) : unseen ? (
            <i className={styles.badge} data-tone="done" />
          ) : null}
        </div>
        {!folded && workspace.subrepos?.length ? (
          <div className={styles.subrepos}>
            {subrepoTree(workspace.subrepos).map((node) => {
              const repo = node.repo >= 0 ? workspace.subrepos![node.repo]! : null
              const marker = node.last ? "└" : "├"
              const indent = { paddingLeft: 42 + node.level * 12 }
              if (!repo) {
                return (
                  <div key={`dir:${node.level}:${node.name}`} className={styles.subrepoFolder} style={indent}>
                    <span className={styles.subrepoMarker}>{marker}</span>
                    {node.name}/
                  </div>
                )
              }
              return (
                <div
                  key={repo.path}
                  className={styles.subrepo}
                  style={indent}
                  title={repo.path}
                  onClick={(event) => onSubrepoMenu(event, index, repo)}
                  onContextMenu={(event) => onSubrepoMenu(event, index, repo)}
                >
                  <span className={styles.subrepoMarker}>{marker}</span>
                  <span className={styles.subrepoName}>{node.name}</span>
                  {repo.branch ? <span className={styles.subrepoBranch}>{repo.branch}</span> : null}
                  <span className={styles.subrepoStats}>
                    {repo.conflicts ? <span data-tone="conflict">!{repo.conflicts}</span> : null}
                    {repo.changed ? <span data-tone="changed">●{repo.changed}</span> : null}
                    {repo.ahead ? <span data-tone="ahead">↑{repo.ahead}</span> : null}
                    {repo.behind ? <span data-tone="behind">↓{repo.behind}</span> : null}
                  </span>
                </div>
              )
            })}
          </div>
        ) : null}
        {!folded
          ? tabs.map((tab, tabIndex) => {
              const panes = [...(tab.panes ?? []), ...(tab.floats ?? [])]
              const activeTab = tabIndex === workspace.active
              return (
                <div key={tabIndex} className={styles.tab}>
                  {tabs.length > 1 ? (
                    <div
                      className={styles.tabRow}
                      data-active={(selected && activeTab) || undefined}
                      onClick={() => report(bridge.tabSelect(index, tabIndex))}
                      onContextMenu={(event) => onTabMenu(event, index, tabIndex)}
                    >
                      {tabLabel(tab, tabIndex)}
                    </div>
                  ) : null}
                  {panes.map((pane) => {
                    const status = paneStatus(pane)
                    const focused = selected && activeTab && tab.focused_pane_id === pane.pane_id
                    return (
                      <div
                        key={pane.pane_id}
                        className={styles.pane}
                        data-focused={focused || undefined}
                        onClick={() => report(bridge.paneFocus(pane.pane_id))}
                        onContextMenu={(event) => onPaneMenu(event, pane)}
                      >
                        <AgentIcon kind={pane.agent || pane.kind} size={14} />
                        <span className={styles.paneName}>{pane.name}</span>
                        <i className={styles.state} data-tone={status.tone} />
                        {pane.width_pct ? <span className={styles.paneKind}>float</span> : null}
                      </div>
                    )
                  })}
                </div>
              )
            })
          : null}
      </div>
    )
  }

  const renderGroup = (node: GroupNode, depth: number, path: string): React.ReactNode => (
    <div key={path} className={styles.group}>
      {depth > 0 && !collapsed ? (
        <div className={styles.groupName} style={{ paddingLeft: 8 + (depth - 1) * 10 }}>
          {node.name}
        </div>
      ) : null}
      <div style={collapsed || depth === 0 ? undefined : { paddingLeft: 6 }}>
        {node.workspaces.map(renderWorkspace)}
        {node.children.map((child) => renderGroup(child, depth + 1, `${path}/${child.name}`))}
      </div>
    </div>
  )

  return (
    <aside className={styles.sidebar} style={{ width: shown }} data-collapsed={collapsed || undefined}>
      <div className={styles.header}>
        <button
          className={styles.iconButton}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          onClick={() => report(bridge.settingsSet({ sidebar_collapsed: !collapsed }))}
        >
          <LineIcon name="sidebar" />
        </button>
      </div>
      {collapsed ? null : (
        <div className={styles.section}>
          <span>Workspaces</span>
          <button className={styles.iconButton} title="New workspace" onClick={() => openModal({ kind: "workspace" })}>
            <LineIcon name="plus" />
          </button>
        </div>
      )}
      <div className={styles.list}>
        {renderGroup(groupTree(workspaces), 0, "")}
        {collapsed ? (
          <button className={`${styles.iconButton} ${styles.compactAdd}`} title="New workspace" onClick={() => openModal({ kind: "workspace" })}>
            <LineIcon name="plus" />
          </button>
        ) : null}
      </div>
      <div className={styles.footer}>
        <button className={styles.footerButton} onClick={() => openModal({ kind: "settings" })} title="Settings">
          <LineIcon name="settings" size={15} />
          {collapsed ? null : <span>Settings</span>}
          {collapsed || !settingsKey ? null : <kbd className={styles.footerKey}>{settingsKey}</kbd>}
        </button>
      </div>
      {collapsed ? null : <div className={styles.resizer} onPointerDown={resize} />}
    </aside>
  )
}
