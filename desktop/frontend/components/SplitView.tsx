"use client"

import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent, type PointerEvent } from "react"
import { bridge, on } from "@/lib/bridge"
import { report } from "@/lib/actions"
import { dropZone, floatRect, layoutTree, ratioAt, withRatio, zoneRect, type Divider, type DropZone, type Rect } from "@/lib/layout"
import type { NodeView, PaneView, TabView } from "@/lib/types"
import PaneFrame from "./PaneFrame"
import styles from "./SplitView.module.css"

interface Props {
  tab: TabView
  path: string
  onPaneMenu: (event: MouseEvent, pane: PaneView) => void
}


export default function SplitView({ tab, path, onPaneMenu }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const [area, setArea] = useState<Rect>({ x: 0, y: 0, w: 0, h: 0 })
  const [drag, setDrag] = useState<{ divider: Divider; ratio: number } | null>(null)
  // A pane:reset remounts that pane's terminal from the replay ring.
  const [generations, setGenerations] = useState<Record<number, number>>({})

  useLayoutEffect(() => {
    const element = host.current
    if (!element) return
    const measure = () => setArea({ x: 0, y: 0, w: element.clientWidth, h: element.clientHeight })
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(
    () =>
      on<{ pane_id: number }>("pane:reset", ({ pane_id }) =>
        setGenerations((current) => ({ ...current, [pane_id]: (current[pane_id] ?? 0) + 1 })),
      ),
    [],
  )

  let root: NodeView | null = tab.layout
  if (drag) root = withRatio(root, drag.divider.path, drag.ratio)
  const { panes: rects, dividers } = layoutTree(root, area)
  const byID = new Map((tab.panes ?? []).map((pane) => [pane.pane_id, pane]))

  // Optimistic drag: render locally, send a throttled update every 100 ms
  // and the final value on release.
  const lastSent = useRef(0)
  const startDrag = (event: PointerEvent, divider: Divider) => {
    event.preventDefault()
    ;(event.target as HTMLElement).setPointerCapture(event.pointerId)
    setDrag({ divider, ratio: divider.ratio })
    const bounds = host.current?.getBoundingClientRect()
    const move = (moveEvent: globalThis.PointerEvent) => {
      if (!bounds) return
      const ratio = ratioAt(divider, moveEvent.clientX - bounds.left, moveEvent.clientY - bounds.top)
      setDrag({ divider, ratio })
      const now = performance.now()
      if (now - lastSent.current > 100) {
        lastSent.current = now
        void bridge.splitSetRatio(divider.path, ratio).catch(() => undefined)
      }
    }
    const up = (upEvent: globalThis.PointerEvent) => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", up)
      if (bounds) {
        const ratio = ratioAt(divider, upEvent.clientX - bounds.left, upEvent.clientY - bounds.top)
        report(bridge.splitSetRatio(divider.path, ratio).finally(() => setDrag(null)))
      } else {
        setDrag(null)
      }
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", up)
  }

  // Moving panes by their title bar: after a 5 px threshold, track the pane
  // under the pointer and its drop zone; release moves or swaps.
  const [move, setMove] = useState<{ pane: number; target: number; zone: DropZone } | null>(null)
  const rectsRef = useRef(rects)
  rectsRef.current = rects
  const startMove = (event: PointerEvent, pane: PaneView) => {
    if (rects.length < 2) return
    const bounds = host.current?.getBoundingClientRect()
    if (!bounds) return
    const startX = event.clientX
    const startY = event.clientY
    let active = false
    let current: { pane: number; target: number; zone: DropZone } | null = null
    const onMove = (moveEvent: globalThis.PointerEvent) => {
      if (!active) {
        if (Math.hypot(moveEvent.clientX - startX, moveEvent.clientY - startY) < 5) return
        active = true
      }
      const x = moveEvent.clientX - bounds.left
      const y = moveEvent.clientY - bounds.top
      const hit = rectsRef.current.find(({ rect }) => x >= rect.x && x < rect.x + rect.w && y >= rect.y && y < rect.y + rect.h)
      current = hit && hit.paneID !== pane.pane_id ? { pane: pane.pane_id, target: hit.paneID, zone: dropZone(hit.rect, x, y) } : null
      setMove(current ?? { pane: pane.pane_id, target: 0, zone: "center" })
    }
    const onUp = () => {
      window.removeEventListener("pointermove", onMove)
      window.removeEventListener("pointerup", onUp)
      setMove(null)
      if (active && current) report(bridge.paneMove(current.pane, current.target, current.zone))
    }
    window.addEventListener("pointermove", onMove)
    window.addEventListener("pointerup", onUp)
  }
  const moveTarget = move?.target ? rects.find((entry) => entry.paneID === move.target) : undefined
  const highlight = moveTarget && move ? zoneRect(moveTarget.rect, move.zone) : undefined

  const floats = tab.floats ?? []

  return (
    <div
      ref={host}
      className={styles.view}
      data-dragging={drag ? (drag.divider.vertical ? "x" : "y") : move ? "move" : undefined}
    >
      {rects.map(({ paneID, rect }) => {
        const pane = byID.get(paneID)
        if (!pane) return null
        return (
          <PaneFrame
            key={paneID}
            pane={pane}
            focused={tab.focused_pane_id === paneID}
            generation={generations[paneID] ?? 0}
            path={path}
            dragging={move?.pane === paneID}
            onTitlePointerDown={startMove}
            style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h }}
            onContextMenu={onPaneMenu}
          />
        )
      })}
      {highlight ? (
        <div
          className={styles.dropZone}
          data-swap={move?.zone === "center" || undefined}
          style={{ left: highlight.x, top: highlight.y, width: highlight.w, height: highlight.h }}
        />
      ) : null}
      {dividers.map((divider) => {
        const { rect } = divider
        const style = { left: rect.x, top: rect.y, width: rect.w, height: rect.h }
        return (
          <div
            key={divider.path.join(".") || "root"}
            className={styles.divider}
            data-vertical={divider.vertical || undefined}
            style={style}
            onPointerDown={(event) => startDrag(event, divider)}
            onDoubleClick={() => report(bridge.splitSetRatio(divider.path, 0.5))}
          />
        )
      })}
      {/* Floats stack above the split view in focus order; later is higher. */}
      {floats.map((pane, index) => {
        const rect = floatRect(area, pane.anchor ?? "center", pane.width_pct ?? 50, pane.height_pct ?? 50)
        return (
          <PaneFrame
            key={pane.pane_id}
            pane={pane}
            floating
            path={path}
            focused={tab.focused_pane_id === pane.pane_id}
            generation={generations[pane.pane_id] ?? 0}
            style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h, zIndex: 20 + index }}
            onContextMenu={onPaneMenu}
          />
        )
      })}
    </div>
  )
}
