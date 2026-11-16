// xterm.js configuration that is independent of React: query suppression and
// the keyboard overrides for keys legacy encoding cannot express.

import type { IDisposable, Terminal } from "@xterm/xterm"

/**
 * The Go VT model answers terminal queries for every pane, including hidden
 * ones. xterm.js must never answer as well, or the application would read
 * two replies. Each handler below swallows a query xterm.js would answer.
 */
export function suppressQueries(term: Pick<Terminal, "parser">): IDisposable {
  const parser = term.parser
  const swallow = () => true
  const disposables: IDisposable[] = [
    // Device attributes: primary, secondary, tertiary.
    parser.registerCsiHandler({ final: "c" }, swallow),
    parser.registerCsiHandler({ prefix: ">", final: "c" }, swallow),
    parser.registerCsiHandler({ prefix: "=", final: "c" }, swallow),
    // Device status and cursor position reports.
    parser.registerCsiHandler({ final: "n" }, swallow),
    parser.registerCsiHandler({ prefix: "?", final: "n" }, swallow),
    // DECRQM mode reports.
    parser.registerCsiHandler({ intermediates: "$", final: "p" }, swallow),
    parser.registerCsiHandler({ prefix: "?", intermediates: "$", final: "p" }, swallow),
    // XTVERSION.
    parser.registerCsiHandler({ prefix: ">", final: "q" }, swallow),
    // Kitty keyboard flags query.
    parser.registerCsiHandler({ prefix: "?", final: "u" }, swallow),
    // Window reports. Only the report variants are swallowed; other window
    // operations are left to xterm.js, which ignores them by default.
    parser.registerCsiHandler({ final: "t" }, (params) => {
      const op = params[0]
      return typeof op === "number" && op >= 11 && op <= 21
    }),
    // DECRQSS and XTGETTCAP.
    parser.registerDcsHandler({ intermediates: "$", final: "q" }, swallow),
    parser.registerDcsHandler({ intermediates: "+", final: "q" }, swallow),
  ]
  // Color queries: OSC 4, 10, 11, 12 with a "?" payload. Color sets pass.
  for (const ident of [10, 11, 12]) {
    disposables.push(parser.registerOscHandler(ident, (data) => data.split(";").includes("?")))
  }
  disposables.push(
    parser.registerOscHandler(4, (data) => {
      const parts = data.split(";")
      for (let i = 1; i < parts.length; i += 2) if (parts[i] === "?") return true
      return false
    }),
  )
  return { dispose: () => disposables.forEach((d) => d.dispose()) }
}

export interface KeyEventLike {
  type: string
  key: string
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  metaKey: boolean
}

/**
 * Bytes for keys xterm.js would encode wrongly for hrdx's children. With
 * kitty keys the CSI u form carries modifiers; without, the legacy bytes
 * match internal/term.EncodeKey and internal/ui legacyEncode. undefined
 * leaves the key to xterm.js.
 */
export function overrideKey(event: KeyEventLike, kitty: boolean, mac = false): string | undefined {
  if (event.type !== "keydown" || event.metaKey) return undefined
  // macOS Option composes characters (Option+L is @ on German layouts).
  // xterm.js leaves these to the textarea input event, which WKWebView does
  // not deliver reliably, so send the composed character directly. Dead
  // keys report "Dead" and still go through composition.
  if (mac && event.altKey && !event.ctrlKey && [...event.key].length === 1) return event.key
  const mods = (event.shiftKey ? 1 : 0) + (event.altKey ? 2 : 0) + (event.ctrlKey ? 4 : 0)
  if (event.key === "Enter") {
    if (kitty && mods !== 0) return `\x1b[13;${mods + 1}u`
    if (event.altKey && !event.ctrlKey) return "\x1b\r"
    if (event.shiftKey && !event.ctrlKey) return "\r"
    return undefined
  }
  if (event.key === "Tab" && kitty && (event.ctrlKey || event.altKey)) {
    return `\x1b[9;${mods + 1}u`
  }
  if (kitty && event.ctrlKey && event.key.length === 1) {
    const code = event.key.charCodeAt(0)
    const isLetter = /[a-zA-Z]/.test(event.key)
    // ctrl+letter keeps its control byte; ctrl+punctuation and digits have
    // no legacy encoding and need CSI u.
    if (!isLetter && code >= 32 && code < 127) {
      return `\x1b[${code};${mods + 1}u`
    }
  }
  return undefined
}

/**
 * Quotes dropped paths for a POSIX shell the way macOS Terminal does:
 * backslash-escape anything outside a safe set, join with spaces, add a
 * trailing space so the next argument can follow.
 */
export function quotePaths(paths: string[]): string {
  return paths.map((path) => path.replace(/[^\p{L}\p{N}_\-.,:+@%/=~]/gu, "\\$&")).join(" ") + " "
}

/** Debounces calls per key with a trailing call. */
export function debounce<A extends unknown[]>(fn: (...args: A) => void, wait: number): (...args: A) => void {
  let timer: ReturnType<typeof setTimeout> | undefined
  let last: A | undefined
  return (...args: A) => {
    last = args
    if (timer) return
    timer = setTimeout(() => {
      timer = undefined
      if (last) fn(...last)
    }, wait)
  }
}

/** Decodes base64 to bytes. */
export function decodeBase64(value: string): Uint8Array {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

/** Encodes a binary string from xterm's onBinary to base64. */
export function encodeBinary(value: string): string {
  return btoa(value)
}

/**
 * Orders live output against a mount snapshot: events at or below the
 * snapshot's seq are already in the replay and are dropped.
 */
export class SeqGate {
  private ready = false
  private seq = 0
  private queued: { seq: number; data: string }[] = []

  constructor(private readonly write: (data: string) => void) {}

  push(seq: number, data: string): void {
    if (!this.ready) {
      this.queued.push({ seq, data })
      return
    }
    if (seq <= this.seq) return
    this.seq = seq
    this.write(data)
  }

  open(seq: number): void {
    this.ready = true
    this.seq = seq
    const queued = this.queued
    this.queued = []
    for (const event of queued.sort((a, b) => a.seq - b.seq)) this.push(event.seq, event.data)
  }
}
