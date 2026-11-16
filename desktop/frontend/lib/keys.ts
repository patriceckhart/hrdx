// Key naming and the prefix state machine. Key names follow Bubble Tea's
// strings so keys.json carries over from the TUI: "ctrl+b", "shift+tab",
// "pgup", "esc", "a", "A", "%".

export interface KeyLike {
  key: string
  code?: string
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  metaKey: boolean
}

const NAMED: Record<string, string> = {
  Enter: "enter",
  Tab: "tab",
  Escape: "esc",
  Backspace: "backspace",
  Delete: "delete",
  ArrowUp: "up",
  ArrowDown: "down",
  ArrowLeft: "left",
  ArrowRight: "right",
  Home: "home",
  End: "end",
  PageUp: "pgup",
  PageDown: "pgdown",
  " ": "space",
}

/** Returns the Bubble Tea style name of a key event, "" for bare modifiers. */
export function keyName(event: KeyLike): string {
  if (["Shift", "Control", "Alt", "Meta", "CapsLock"].includes(event.key)) return ""
  let base = NAMED[event.key]
  if (base === undefined) {
    base = event.key
    // With ctrl or alt held, browsers report layout characters; prefer the
    // physical letter so ctrl+b is ctrl+b on every layout with Latin keys.
    if ((event.ctrlKey || event.altKey) && event.code?.startsWith("Key")) {
      base = event.code.slice(3).toLowerCase()
      if (event.shiftKey && !event.ctrlKey) base = base.toUpperCase()
    }
  }
  const parts: string[] = []
  if (event.ctrlKey) parts.push("ctrl")
  if (event.altKey) parts.push("alt")
  if (event.metaKey) parts.push("cmd")
  // Shift is implied by printable characters ("A", "%"); keep it for named keys.
  if (event.shiftKey && (NAMED[event.key] !== undefined || event.ctrlKey)) parts.push("shift")
  if (base === "tab" && event.shiftKey && !event.ctrlKey && !event.altKey && !event.metaKey) return "shift+tab"
  parts.push(base.length === 1 && event.ctrlKey ? base.toLowerCase() : base)
  return parts.join("+")
}

/** Control byte for a ctrl+letter chord, used for the literal prefix. */
export function controlByte(name: string): string | undefined {
  const match = /^ctrl\+([a-z])$/.exec(name)
  if (!match?.[1]) return undefined
  return String.fromCharCode(match[1].charCodeAt(0) - 96)
}

export type PrefixResult =
  | { kind: "pass" } // not ours: the terminal handles the key
  | { kind: "enter" } // prefix pressed, waiting for an action key
  | { kind: "action"; action: string; stay: boolean }
  | { kind: "cancel" }

/**
 * Prefix state machine. The prefix key enters prefix mode; the next key is
 * looked up in the keymap. tab and shift+tab keep prefix mode active for
 * repeated pane jumps until esc. Unknown keys leave without side effects.
 */
export class PrefixMachine {
  active = false
  private sticky = false

  constructor(
    private prefix: string,
    private keymap: Record<string, string>,
  ) {}

  configure(prefix: string, keymap: Record<string, string>): void {
    this.prefix = prefix
    this.keymap = keymap
  }

  reset(): void {
    this.active = false
    this.sticky = false
  }

  handle(name: string): PrefixResult {
    if (name === "") return this.active ? { kind: "cancel" } : { kind: "pass" }
    if (!this.active) {
      if (name === this.prefix) {
        this.active = true
        return { kind: "enter" }
      }
      return { kind: "pass" }
    }
    if (this.sticky && name === "esc") {
      this.reset()
      return { kind: "cancel" }
    }
    const action = this.keymap[name]
    if (!action) {
      this.reset()
      return { kind: "cancel" }
    }
    const stay = action === "pane-next" || action === "pane-prev"
    this.sticky = stay
    this.active = stay
    return { kind: "action", action, stay }
  }
}
