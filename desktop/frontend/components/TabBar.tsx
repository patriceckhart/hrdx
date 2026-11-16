"use client"

import type { MouseEvent } from "react"
import { bridge } from "@/lib/bridge"
import { report, splitPicker } from "@/lib/actions"
import { tabLabel } from "@/lib/store"
import type { PaneView, WorkspaceView } from "@/lib/types"
import { openModal } from "@/lib/ui"
import { AgentIcon, LineIcon } from "./Icons"
import styles from "./TabBar.module.css"

interface Props {
  workspace: WorkspaceView
  index: number
  onTabMenu: (event: MouseEvent, workspace: number, tab: number) => void
}

function tabState(panes: PaneView[]): "busy" | "attention" | "done" | "exited" | "" {
  if (panes.some((pane) => pane.attention)) return "attention"
  if (panes.some((pane) => pane.busy)) return "busy"
  if (panes.some((pane) => pane.unseen)) return "done"
  return ""
}

/** The window's top row: tabs as pills, sitting in the transparent title bar. */
export default function TabBar({ workspace, index, onTabMenu }: Props) {
  const tabs = workspace.tabs ?? []
  return (
    <div className={styles.bar}>
      <div className={styles.tabs}>
        {tabs.map((tab, tabIndex) => {
          const panes = [...(tab.panes ?? []), ...(tab.floats ?? [])]
          const first = panes[0]
          const state = tabState(panes)
          const active = tabIndex === workspace.active
          return (
            <button
              key={tabIndex}
              className={styles.tab}
              data-active={active || undefined}
              onClick={() => report(bridge.tabSelect(index, tabIndex))}
              onAuxClick={(event) => {
                if (event.button === 1 && tabs.length > 1) report(bridge.tabClose(index, tabIndex))
              }}
              onContextMenu={(event) => onTabMenu(event, index, tabIndex)}
            >
              <AgentIcon kind={first?.agent || first?.kind || "shell"} size={15} />
              <span className={styles.label}>{tab.name || tabLabel(tab, tabIndex).replace(/^\d+ /, "")}</span>
              {state === "busy" ? <i className={styles.spinner} /> : state ? <i className={styles.dot} data-tone={state} /> : null}
              {tabs.length > 1 ? (
                <span
                  className={styles.close}
                  title="Close tab"
                  onClick={(event) => {
                    event.stopPropagation()
                    report(bridge.tabClose(index, tabIndex))
                  }}
                >
                  ×
                </span>
              ) : null}
            </button>
          )
        })}
      </div>
      <div className={styles.actions}>
        <button className={styles.action} title="New tab" onClick={() => splitPicker("tab")}>
          <LineIcon name="plus" />
        </button>
        <button className={styles.action} title="Find" onClick={() => openModal({ kind: "finder" })}>
          <LineIcon name="search" />
        </button>
      </div>
    </div>
  )
}
