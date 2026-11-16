"use client"

import dynamic from "next/dynamic"
import type { CSSProperties, MouseEvent, PointerEvent } from "react"
import { bridge } from "@/lib/bridge"
import { report } from "@/lib/actions"
import type { PaneView } from "@/lib/types"
import { setFocusedTerminal } from "@/lib/ui"
import { AgentIcon, LineIcon } from "./Icons"
import styles from "./PaneFrame.module.css"

// xterm.js needs the DOM; the static prerender runs in Node.
const Terminal = dynamic(() => import("./Terminal"), { ssr: false })

/** Minimum content size in pixels for 2 columns by 2 rows at small fonts. */
const MIN_CONTENT_W = 24
const MIN_CONTENT_H = 40

interface Props {
  pane: PaneView
  focused: boolean
  style: CSSProperties
  generation: number
  floating?: boolean
  path?: string
  dragging?: boolean
  onTitlePointerDown?: (event: PointerEvent, pane: PaneView) => void
  onContextMenu: (event: MouseEvent, pane: PaneView) => void
}

export function paneStatus(pane: PaneView): { label: string; tone: "busy" | "attention" | "done" | "exited" | "idle" } {
  if (pane.failure) return { label: "failed", tone: "exited" }
  if (pane.starting) return { label: "starting", tone: "idle" }
  if (!pane.running) return { label: "exited", tone: "exited" }
  if (pane.attention) return { label: "waiting", tone: "attention" }
  if (pane.busy) return { label: "working", tone: "busy" }
  if (pane.unseen) return { label: "finished", tone: "done" }
  return { label: "", tone: "idle" }
}

function shortPath(path: string): string {
  return path.replace(/^\/(Users|home)\/[^/]+/, "~")
}

const AGENT_NAMES: Record<string, string> = { claude: "Claude Code", codex: "Codex", zot: "zot", pi: "pi", shell: "Shell" }

export default function PaneFrame({ pane, focused, style, generation, floating, path, dragging, onTitlePointerDown, onContextMenu }: Props) {
  const width = typeof style.width === "number" ? style.width : 0
  const height = typeof style.height === "number" ? style.height : 0
  const tooSmall = width - 20 < MIN_CONTENT_W || height - 52 < MIN_CONTENT_H
  const status = paneStatus(pane)
  if (focused) setFocusedTerminal(pane.pane_id)

  const focus = () => {
    if (!focused) report(bridge.paneFocus(pane.pane_id))
  }
  const agent = pane.agent || pane.kind
  const defaultName = new RegExp(`^${pane.kind} \\d+$`).test(pane.name)
  const label = defaultName ? (AGENT_NAMES[agent] ?? agent) : pane.name

  return (
    <div
      className={styles.frame}
      data-focused={focused || undefined}
      data-floating={floating || undefined}
      data-pane-id={pane.pane_id}
      data-dragging={dragging || undefined}
      style={style}
      onMouseDownCapture={focus}
      onContextMenu={(event) => onContextMenu(event, pane)}
    >
      <div
        className={styles.title}
        data-movable={onTitlePointerDown ? true : undefined}
        onPointerDown={(event) => {
          if (event.button === 0 && !(event.target as HTMLElement).closest("button")) onTitlePointerDown?.(event, pane)
        }}
      >
        <AgentIcon kind={agent} size={16} />
        <span className={styles.name}>{label}</span>
        <span className={styles.path} title={pane.title || path}>
          {pane.title && !defaultName ? pane.title : path ? shortPath(path) : ""}
        </span>
        {status.label ? <span className={styles.status} data-tone={status.tone}>{status.label}</span> : null}
        <span className={styles.dot} data-tone={status.tone} title={status.label || "running"} />
        <button
          className={styles.close}
          title="Close pane"
          onMouseDown={(event) => event.stopPropagation()}
          onClick={() => report(bridge.paneClose(pane.pane_id))}
        >
          <LineIcon name="close" size={13} />
        </button>
      </div>
      <div className={styles.body}>
        {pane.failure ? (
          <div className={styles.failure}>
            <p>{pane.failure}</p>
            {pane.failure.startsWith("Durable") ? null : (
              <button onClick={() => report(bridge.paneRestart(pane.pane_id))}>Restart</button>
            )}
          </div>
        ) : tooSmall ? null : (
          <Terminal key={`${pane.pane_id}:${generation}`} paneID={pane.pane_id} focused={focused} kittyKeys={Boolean(pane.kitty_keys)} />
        )}
        {!pane.running && !pane.starting && !pane.failure ? (
          <button className={styles.restart} onClick={() => report(bridge.paneRestart(pane.pane_id))}>
            Process exited. Restart
          </button>
        ) : null}
      </div>
    </div>
  )
}
