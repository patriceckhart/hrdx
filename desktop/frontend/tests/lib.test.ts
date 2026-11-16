import { describe, expect, test } from "bun:test"
import { controlByte, keyName, PrefixMachine } from "../lib/keys"
import { DIVIDER, dropZone, floatRect, layoutTree, ratioAt, withRatio, zoneRect } from "../lib/layout"
import { ansi256, cssColor, cssVariables, xtermTheme } from "../lib/theme"
import type { NodeView } from "../lib/types"
import { reorderTarget } from "../lib/reorder"

// Mirrors core.WorkspaceReorder: remove `from`, then insert at `to`.
const moved = (items: string[], from: number, to: number | null) => {
  if (to === null) return items
  const next = items.filter((_, index) => index !== from)
  next.splice(to, 0, items[from]!)
  return next
}

describe("workspace reorder", () => {
  const items = ["a", "b", "c", "d"]
  test("lands exactly where dropped", () => {
    expect(moved(items, 0, reorderTarget(0, 2, false))).toEqual(["b", "a", "c", "d"])
    expect(moved(items, 0, reorderTarget(0, 2, true))).toEqual(["b", "c", "a", "d"])
    expect(moved(items, 0, reorderTarget(0, 3, true))).toEqual(["b", "c", "d", "a"])
    expect(moved(items, 3, reorderTarget(3, 0, false))).toEqual(["d", "a", "b", "c"])
    expect(moved(items, 3, reorderTarget(3, 1, true))).toEqual(["a", "b", "d", "c"])
  })
  test("no-op drops are ignored", () => {
    expect(reorderTarget(1, 1, false)).toBeNull()
    expect(reorderTarget(1, 1, true)).toBeNull()
    expect(reorderTarget(1, 0, true)).toBeNull()
    expect(reorderTarget(1, 2, false)).toBeNull()
    expect(reorderTarget(-1, 2, false)).toBeNull()
  })
})

const ev = (key: string, mods: Partial<Record<"ctrlKey" | "altKey" | "shiftKey" | "metaKey", boolean>> = {}, code?: string) => ({
  key,
  code,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  metaKey: false,
  ...mods,
})

describe("key names", () => {
  test("match Bubble Tea strings", () => {
    expect(keyName(ev("b", { ctrlKey: true }, "KeyB"))).toBe("ctrl+b")
    expect(keyName(ev("A", { shiftKey: true }, "KeyA"))).toBe("A")
    expect(keyName(ev("%", { shiftKey: true }, "Digit5"))).toBe("%")
    expect(keyName(ev("Tab", { shiftKey: true }))).toBe("shift+tab")
    expect(keyName(ev("Escape"))).toBe("esc")
    expect(keyName(ev("PageUp"))).toBe("pgup")
    expect(keyName(ev("Shift", { shiftKey: true }))).toBe("")
    expect(keyName(ev("∫", { altKey: true }, "KeyB"))).toBe("alt+b")
  })

  test("control bytes", () => {
    expect(controlByte("ctrl+b")).toBe("\x02")
    expect(controlByte("ctrl+a")).toBe("\x01")
    expect(controlByte("q")).toBeUndefined()
  })
})

describe("prefix machine", () => {
  const keymap = { c: "picker-right", tab: "pane-next", "shift+tab": "pane-prev", "ctrl+b": "literal", q: "quit" }

  test("dispatches one action and exits", () => {
    const m = new PrefixMachine("ctrl+b", keymap)
    expect(m.handle("x")).toEqual({ kind: "pass" })
    expect(m.handle("ctrl+b")).toEqual({ kind: "enter" })
    expect(m.handle("c")).toEqual({ kind: "action", action: "picker-right", stay: false })
    expect(m.active).toBe(false)
  })

  test("prefix twice sends the literal", () => {
    const m = new PrefixMachine("ctrl+b", keymap)
    m.handle("ctrl+b")
    expect(m.handle("ctrl+b")).toEqual({ kind: "action", action: "literal", stay: false })
  })

  test("tab stays until esc", () => {
    const m = new PrefixMachine("ctrl+b", keymap)
    m.handle("ctrl+b")
    expect(m.handle("tab")).toMatchObject({ action: "pane-next", stay: true })
    expect(m.handle("shift+tab")).toMatchObject({ action: "pane-prev", stay: true })
    expect(m.handle("esc")).toEqual({ kind: "cancel" })
    expect(m.active).toBe(false)
  })

  test("unknown key cancels without side effects", () => {
    const m = new PrefixMachine("ctrl+b", keymap)
    m.handle("ctrl+b")
    expect(m.handle("z")).toEqual({ kind: "cancel" })
    expect(m.handle("c")).toEqual({ kind: "pass" })
  })

  test("option compositions are not prefix keys", () => {
    const m = new PrefixMachine("ctrl+b", keymap)
    expect(m.handle(keyName(ev("@", { altKey: true }, "KeyL")))).toEqual({ kind: "pass" })
  })

  test("remapped prefix", () => {
    const m = new PrefixMachine("ctrl+a", keymap)
    expect(m.handle("ctrl+b")).toEqual({ kind: "pass" })
    expect(m.handle("ctrl+a")).toEqual({ kind: "enter" })
  })
})

describe("split layout", () => {
  const tree: NodeView = {
    vertical: true,
    ratio: 0.5,
    a: { pane_id: 1 },
    b: { ratio: 0.25, a: { pane_id: 2 }, b: { pane_id: 3 } },
  }

  test("every leaf once, inside the area, no overlap", () => {
    const area = { x: 0, y: 0, w: 1008, h: 608 }
    const { panes, dividers } = layoutTree(tree, area)
    expect(panes.map((p) => p.paneID)).toEqual([1, 2, 3])
    expect(dividers.map((d) => d.path)).toEqual([[], [1]])
    const [a, b, c] = panes.map((p) => p.rect)
    expect(a!.w + DIVIDER + b!.w).toBe(1008)
    expect(b!.x).toBe(a!.w + DIVIDER)
    expect(b!.h + DIVIDER + c!.h).toBe(608)
    expect(b!.h).toBe(150)
  })

  test("minimum sizes win over ratios", () => {
    const { panes } = layoutTree({ vertical: true, ratio: 0.01, a: { pane_id: 1 }, b: { pane_id: 2 } }, { x: 0, y: 0, w: 500, h: 100 })
    expect(panes[0]!.rect.w).toBeGreaterThanOrEqual(48)
  })

  test("ratio from pointer is clamped", () => {
    const { dividers } = layoutTree(tree, { x: 0, y: 0, w: 1000, h: 600 })
    expect(ratioAt(dividers[0]!, 250, 0)).toBeCloseTo(0.25)
    expect(ratioAt(dividers[0]!, -50, 0)).toBe(0.1)
  })

  test("withRatio updates one node", () => {
    const next = withRatio(tree, [1], 0.7)
    expect(next?.b?.ratio).toBe(0.7)
    expect(tree.b?.ratio).toBe(0.25)
  })

  test("drop zones", () => {
    const rect = { x: 100, y: 100, w: 400, h: 200 }
    expect(dropZone(rect, 110, 200)).toBe("left")
    expect(dropZone(rect, 490, 200)).toBe("right")
    expect(dropZone(rect, 300, 105)).toBe("top")
    expect(dropZone(rect, 300, 295)).toBe("bottom")
    expect(dropZone(rect, 300, 200)).toBe("center")
    expect(dropZone(rect, 101, 110)).toBe("left")
    expect(zoneRect(rect, "right")).toEqual({ x: 300, y: 100, w: 200, h: 200 })
    expect(zoneRect(rect, "bottom")).toEqual({ x: 100, y: 200, w: 400, h: 100 })
  })

  test("float anchors", () => {
    const area = { x: 10, y: 20, w: 1000, h: 500 }
    expect(floatRect(area, "left", 40, 30)).toEqual({ x: 10, y: 195, w: 400, h: 150 })
    expect(floatRect(area, "bottom", 50, 50).y).toBe(270)
    expect(floatRect(area, "center", 1, 1).w).toBe(96)
  })
})

describe("theme mapping", () => {
  test("ansi 256", () => {
    expect(ansi256(1)).toBe("#cd0000")
    expect(ansi256(81)).toBe("#5fd7ff")
    expect(ansi256(232)).toBe("#080808")
    expect(ansi256(255)).toBe("#eeeeee")
  })

  test("css colors", () => {
    expect(cssColor("#ABCDEF", "x")).toBe("#abcdef")
    expect(cssColor("999", "fallback")).toBe("fallback")
    expect(cssColor("red; background: url(x)", "fallback")).toBe("fallback")
    const vars = cssVariables({ accent: "#bd93f9", bar_bg: "234" })
    expect(vars["--hrdx-accent"]).toBe("#bd93f9")
    expect(vars["--hrdx-bar-bg"]).toBe("#1c1c1c")
    expect(vars["--hrdx-good"]).toBe(ansi256(78))
    expect(xtermTheme({ accent: "#123456" }).cursor).toBe("#123456")
  })
})
