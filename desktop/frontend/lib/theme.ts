// Maps an hrdx theme palette (ANSI 256 numbers or #rrggbb) to CSS custom
// properties for the chrome and an xterm.js theme for terminals.

import type { ITheme } from "@xterm/xterm"

const ANSI16 = [
  "#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
  "#7f7f7f", "#ff0000", "#00ff00", "#ffff00", "#5c5cff", "#ff00ff", "#00ffff", "#ffffff",
]

function hex(value: number): string {
  return value.toString(16).padStart(2, "0")
}

/** Converts an xterm 256-color index to #rrggbb. */
export function ansi256(index: number): string {
  if (index < 16) return ANSI16[index] ?? "#000000"
  if (index < 232) {
    const n = index - 16
    const level = (v: number) => (v === 0 ? 0 : 55 + v * 40)
    return `#${hex(level(Math.floor(n / 36)))}${hex(level(Math.floor(n / 6) % 6))}${hex(level(n % 6))}`
  }
  const gray = 8 + (index - 232) * 10
  return `#${hex(gray)}${hex(gray)}${hex(gray)}`
}

/** Resolves a palette value to a CSS color. Invalid values become fallback. */
export function cssColor(value: string | undefined, fallback: string): string {
  if (!value) return fallback
  const trimmed = value.trim()
  if (/^\d{1,3}$/.test(trimmed)) {
    const n = Number(trimmed)
    return n <= 255 ? ansi256(n) : fallback
  }
  if (/^#[0-9a-fA-F]{6}$/.test(trimmed) || /^#[0-9a-fA-F]{3}$/.test(trimmed)) return trimmed.toLowerCase()
  return fallback
}

export const ROLES = ["accent", "alt", "muted", "faint", "good", "busy", "bad", "bar_bg", "bar_fg", "ink"] as const

const DEFAULTS: Record<(typeof ROLES)[number], string> = {
  accent: "81", alt: "212", muted: "243", faint: "238", good: "78",
  busy: "214", bad: "203", bar_bg: "234", bar_fg: "250", ink: "232",
}

/** CSS custom properties for the chrome, --hrdx-<role>. */
export function cssVariables(colors: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const role of ROLES) {
    out[`--hrdx-${role.replace("_", "-")}`] = cssColor(colors[role], cssColor(DEFAULTS[role], "#888888"))
  }
  return out
}

/** Pane card background; terminals blend into it. Keep in sync with --surface. */
export const SURFACE = "#18181b"

/** xterm.js theme. ANSI colors keep xterm defaults; chrome roles tint it. */
export function xtermTheme(colors: Record<string, string>): ITheme {
  const vars = cssVariables(colors)
  return {
    background: SURFACE,
    foreground: "#e4e4e7",
    cursor: vars["--hrdx-accent"],
    cursorAccent: SURFACE,
    selectionBackground: `${vars["--hrdx-accent"]}55`,
  }
}
