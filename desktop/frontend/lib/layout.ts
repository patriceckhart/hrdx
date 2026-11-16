// Split tree geometry in CSS pixels. vertical keeps its state.Node meaning:
// children side by side, divided by a vertical line.

import type { NodeView } from "./types"

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export interface PaneRect {
  paneID: number
  rect: Rect
}

export interface Divider {
  path: number[]
  vertical: boolean
  ratio: number
  /** The divider line itself. */
  rect: Rect
  /** The split's whole area, for converting a pointer to a ratio. */
  area: Rect
}

/** Gap between pane cards; the divider hit area sits in it. */
export const DIVIDER = 8
export const MIN_PANE = 48

function clamp(value: number, low: number, high: number): number {
  if (high < low) return low
  return Math.min(Math.max(value, low), high)
}

function ratioOf(node: NodeView): number {
  const ratio = node.ratio ?? 0.5
  return ratio > 0 && ratio < 1 ? ratio : 0.5
}

/** Child areas of a split, with a 1 px divider between them. */
export function splitRects(node: NodeView, area: Rect): { a: Rect; divider: Rect; b: Rect } {
  if (node.vertical) {
    const usable = Math.max(0, area.w - DIVIDER)
    const aw = Math.round(clamp(usable * ratioOf(node), Math.min(MIN_PANE, usable / 2), usable - Math.min(MIN_PANE, usable / 2)))
    return {
      a: { x: area.x, y: area.y, w: aw, h: area.h },
      divider: { x: area.x + aw, y: area.y, w: DIVIDER, h: area.h },
      b: { x: area.x + aw + DIVIDER, y: area.y, w: usable - aw, h: area.h },
    }
  }
  const usable = Math.max(0, area.h - DIVIDER)
  const ah = Math.round(clamp(usable * ratioOf(node), Math.min(MIN_PANE, usable / 2), usable - Math.min(MIN_PANE, usable / 2)))
  return {
    a: { x: area.x, y: area.y, w: area.w, h: ah },
    divider: { x: area.x, y: area.y + ah, w: area.w, h: DIVIDER },
    b: { x: area.x, y: area.y + ah + DIVIDER, w: area.w, h: usable - ah },
  }
}

/** Lays out a whole tree. Every leaf appears once, in tree order. */
export function layoutTree(root: NodeView | null | undefined, area: Rect): { panes: PaneRect[]; dividers: Divider[] } {
  const panes: PaneRect[] = []
  const dividers: Divider[] = []
  const walk = (node: NodeView | undefined, rect: Rect, path: number[]) => {
    if (!node) return
    if (node.a === undefined || node.b === undefined) {
      if (node.pane_id !== undefined) panes.push({ paneID: node.pane_id, rect })
      return
    }
    const parts = splitRects(node, rect)
    dividers.push({ path, vertical: Boolean(node.vertical), ratio: ratioOf(node), rect: parts.divider, area: rect })
    walk(node.a, parts.a, [...path, 0])
    walk(node.b, parts.b, [...path, 1])
  }
  walk(root ?? undefined, area, [])
  return { panes, dividers }
}

/** Converts a pointer position over a split to a clamped ratio. */
export function ratioAt(divider: Divider, x: number, y: number): number {
  const { area } = divider
  const ratio = divider.vertical ? (x - area.x) / Math.max(1, area.w) : (y - area.y) / Math.max(1, area.h)
  return clamp(ratio, 0.1, 0.9)
}

/** Places a floating pane like the TUI: anchored, sized by percentage. */
export function floatRect(area: Rect, anchor: string, widthPct: number, heightPct: number): Rect {
  const w = clamp(Math.round((area.w * widthPct) / 100), Math.min(MIN_PANE * 2, area.w), area.w)
  const h = clamp(Math.round((area.h * heightPct) / 100), Math.min(MIN_PANE * 2, area.h), area.h)
  let x = area.x + Math.round((area.w - w) / 2)
  let y = area.y + Math.round((area.h - h) / 2)
  switch (anchor) {
    case "top":
      y = area.y
      break
    case "bottom":
      y = area.y + area.h - h
      break
    case "left":
      x = area.x
      break
    case "right":
      x = area.x + area.w - w
      break
  }
  return { x, y, w, h }
}

export type DropZone = "left" | "right" | "top" | "bottom" | "center"

/**
 * Drop zone for a pointer over a pane: the outer quarter on each side splits
 * there, the middle swaps. The nearest edge wins in the corners.
 */
export function dropZone(rect: Rect, x: number, y: number): DropZone {
  const fx = (x - rect.x) / Math.max(1, rect.w)
  const fy = (y - rect.y) / Math.max(1, rect.h)
  const edges: [DropZone, number][] = [
    ["left", fx],
    ["right", 1 - fx],
    ["top", fy],
    ["bottom", 1 - fy],
  ]
  const [zone, distance] = edges.reduce((best, edge) => (edge[1] < best[1] ? edge : best))
  return distance < 0.25 ? zone : "center"
}

/** Highlight rectangle for a drop zone, inside the target pane. */
export function zoneRect(rect: Rect, zone: DropZone): Rect {
  switch (zone) {
    case "left":
      return { ...rect, w: rect.w / 2 }
    case "right":
      return { ...rect, x: rect.x + rect.w / 2, w: rect.w / 2 }
    case "top":
      return { ...rect, h: rect.h / 2 }
    case "bottom":
      return { ...rect, y: rect.y + rect.h / 2, h: rect.h / 2 }
    case "center":
      return rect
  }
}

/** Replaces the ratio at path, for optimistic drag rendering. */
export function withRatio(root: NodeView | null, path: number[], ratio: number): NodeView | null {
  if (!root) return root
  if (path.length === 0) return { ...root, ratio }
  const [head, ...rest] = path
  if (head === 0 && root.a) return { ...root, a: withRatio(root.a, rest, ratio) ?? undefined }
  if (head === 1 && root.b) return { ...root, b: withRatio(root.b, rest, ratio) ?? undefined }
  return root
}
