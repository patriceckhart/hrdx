import { describe, expect, test } from "bun:test"
import { Terminal } from "@xterm/headless"
import { overrideKey, quotePaths, SeqGate, suppressQueries } from "../lib/xterm"

function write(term: Terminal, data: string): Promise<void> {
  return new Promise((resolve) => term.write(data, resolve))
}

// Every query xterm.js answers by itself. The Go VT model is the only
// responder, so none may produce onData.
const QUERIES: Record<string, string> = {
  "primary DA": "\x1b[c",
  "primary DA 0": "\x1b[0c",
  "secondary DA": "\x1b[>c",
  "tertiary DA": "\x1b[=c",
  "device status": "\x1b[5n",
  "cursor position": "\x1b[6n",
  "DEC cursor position": "\x1b[?6n",
  "ANSI DECRQM": "\x1b[4$p",
  "DEC DECRQM": "\x1b[?2004$p",
  XTVERSION: "\x1b[>0q",
  "kitty keyboard query": "\x1b[?u",
  "window size pixels": "\x1b[14t",
  "window size chars": "\x1b[18t",
  "screen size chars": "\x1b[19t",
  "window title": "\x1b[21t",
  "OSC 4 palette": "\x1b]4;1;?\x07",
  "OSC 10 foreground": "\x1b]10;?\x07",
  "OSC 11 background": "\x1b]11;?\x1b\\",
  "OSC 12 cursor": "\x1b]12;?\x07",
  DECRQSS: "\x1bP$qm\x1b\\",
  XTGETTCAP: "\x1bP+q544e\x1b\\",
}

describe("query suppression", () => {
  test("unsuppressed xterm answers queries", async () => {
    const term = new Terminal({ allowProposedApi: true })
    let answered = ""
    term.onData((data) => (answered += data))
    await write(term, QUERIES["primary DA"]!)
    expect(answered).not.toBe("")
  })

  for (const [name, sequence] of Object.entries(QUERIES)) {
    test(`swallows ${name}`, async () => {
      const term = new Terminal({ allowProposedApi: true })
      suppressQueries(term)
      let answered = ""
      term.onData((data) => (answered += data))
      term.onBinary((data) => (answered += data))
      await write(term, sequence)
      expect(answered).toBe("")
    })
  }

  test("color sets and ordinary output still work", async () => {
    const term = new Terminal({ allowProposedApi: true, cols: 20, rows: 2 })
    suppressQueries(term)
    await write(term, "\x1b]11;#102030\x07hello")
    expect(term.buffer.active.getLine(0)?.translateToString(true)).toBe("hello")
  })
})

const key = (key: string, mods: Partial<Record<"ctrlKey" | "altKey" | "shiftKey" | "metaKey", boolean>> = {}) => ({
  type: "keydown",
  key,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  metaKey: false,
  ...mods,
})

describe("key overrides", () => {
  // Expected bytes match internal/term.EncodeKey (legacy) and the CSI u
  // encoding hrdx forwards to kitty-aware children.
  test("legacy encodings", () => {
    expect(overrideKey(key("Enter", { altKey: true }), false)).toBe("\x1b\r")
    expect(overrideKey(key("Enter", { shiftKey: true }), false)).toBe("\r")
    expect(overrideKey(key("Enter"), false)).toBeUndefined()
    expect(overrideKey(key("Tab", { ctrlKey: true }), false)).toBeUndefined()
    expect(overrideKey(key("1", { ctrlKey: true }), false)).toBeUndefined()
  })

  test("macOS option composes characters", () => {
    expect(overrideKey(key("@", { altKey: true }), false, true)).toBe("@")
    expect(overrideKey(key("€", { altKey: true }), true, true)).toBe("€")
    expect(overrideKey(key("Dead", { altKey: true }), false, true)).toBeUndefined()
    expect(overrideKey(key("@", { altKey: true }), false, false)).toBeUndefined()
    expect(overrideKey(key("Enter", { altKey: true }), false, true)).toBe("\x1b\r")
  })

  test("kitty encodings", () => {
    expect(overrideKey(key("Enter", { shiftKey: true }), true)).toBe("\x1b[13;2u")
    expect(overrideKey(key("Enter", { altKey: true }), true)).toBe("\x1b[13;3u")
    expect(overrideKey(key("Enter", { ctrlKey: true }), true)).toBe("\x1b[13;5u")
    expect(overrideKey(key("Tab", { ctrlKey: true, shiftKey: true }), true)).toBe("\x1b[9;6u")
    expect(overrideKey(key("1", { ctrlKey: true }), true)).toBe("\x1b[49;5u")
    expect(overrideKey(key(".", { ctrlKey: true }), true)).toBe("\x1b[46;5u")
    expect(overrideKey(key("a", { ctrlKey: true }), true)).toBeUndefined()
    expect(overrideKey(key("Enter"), true)).toBeUndefined()
  })

  test("option compositions stay with xterm", () => {
    // Option+L types @ on German layouts; Option+5 types [.
    expect(overrideKey(key("@", { altKey: true }), false)).toBeUndefined()
    expect(overrideKey(key("[", { altKey: true }), true)).toBeUndefined()
  })

  test("cmd chords and keyup pass through", () => {
    expect(overrideKey(key("Enter", { metaKey: true, shiftKey: true }), true)).toBeUndefined()
    expect(overrideKey({ ...key("Enter", { shiftKey: true }), type: "keyup" }, true)).toBeUndefined()
  })
})

describe("dropped paths", () => {
  test("escape shell metacharacters like Terminal", () => {
    expect(quotePaths(["/Users/x/a.png"])).toBe("/Users/x/a.png ")
    expect(quotePaths(["/tmp/My File (1).txt", "/tmp/b's$x"])).toBe("/tmp/My\\ File\\ \\(1\\).txt /tmp/b\\'s\\$x ")
    expect(quotePaths(["/tmp/Größe.txt"])).toBe("/tmp/Größe.txt ")
  })
})

describe("seq gate", () => {
  test("drops events covered by the replay and orders the rest", () => {
    const out: string[] = []
    const gate = new SeqGate((data) => out.push(data))
    gate.push(4, "d")
    gate.push(2, "b")
    gate.push(3, "c")
    gate.open(3)
    gate.push(3, "c-again")
    gate.push(5, "e")
    expect(out).toEqual(["d", "e"])
  })
})
