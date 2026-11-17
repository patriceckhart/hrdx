"use client"

import { useEffect, useRef } from "react"
import { Terminal as XTerm } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { Unicode11Addon } from "@xterm/addon-unicode11"
import { WebLinksAddon } from "@xterm/addon-web-links"
import "@xterm/xterm/css/xterm.css"
import { bridge, on } from "@/lib/bridge"
import { getView } from "@/lib/store"
import { xtermTheme } from "@/lib/theme"
import type { OutputEvent } from "@/lib/types"
import { keyName } from "@/lib/keys"
import { copiedToast, registerTerminal } from "@/lib/ui"
import { debounce, decodeBase64, encodeBinary, normalizePaste, overrideKey, SeqGate, suppressQueries } from "@/lib/xterm"
import { handleTerminalKey } from "./KeyRouter"
import styles from "./Terminal.module.css"

const MONO = 'ui-monospace, "SF Mono", Menlo, Monaco, "Cascadia Mono", "DejaVu Sans Mono", Consolas, monospace'
const IS_MAC = typeof navigator !== "undefined" && /Mac/.test(navigator.platform)

interface Props {
  paneID: number
  focused: boolean
  kittyKeys: boolean
}

/**
 * One xterm.js instance bound to a pane. Output arrives as pane:output events
 * and is written straight into xterm.js, never through React state. The
 * component remounts on pane:reset and replays from the bridge ring.
 */
export default function Terminal({ paneID, focused, kittyKeys }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<XTerm | null>(null)
  const kitty = useRef(kittyKeys)
  kitty.current = kittyKeys

  useEffect(() => {
    const element = host.current
    if (!element) return
    const view = getView()
    const prefs = view?.prefs
    const xterm = new XTerm({
      allowProposedApi: true,
      fontFamily: prefs?.font_family ? `${prefs.font_family}, ${MONO}` : MONO,
      fontSize: prefs?.font_size ?? 13,
      lineHeight: prefs?.line_height ?? 1.2,
      cursorBlink: prefs?.cursor_blink ?? true,
      scrollback: prefs?.scrollback_lines ?? 10000,
      theme: xtermTheme(view?.theme.colors ?? {}),
      // Option composes characters (@ is Option+L on German layouts), as in
      // macOS Terminal's default. Terminals send no meta for it.
      macOptionIsMeta: false,
      rightClickSelectsWord: false,
    })
    term.current = xterm
    const fit = new FitAddon()
    xterm.loadAddon(fit)
    const unicode = new Unicode11Addon()
    xterm.loadAddon(unicode)
    xterm.unicode.activeVersion = "11"
    xterm.loadAddon(
      new WebLinksAddon((event, uri) => {
        if (event.metaKey || event.ctrlKey) void bridge.openExternal(uri).catch(() => undefined)
      }),
    )
    const queries = suppressQueries(xterm)
    xterm.open(element)

    // WebGL renders fastest; the DOM renderer is the fallback when the
    // context is unavailable or lost (common on Linux WebKitGTK).
    let disposed = false
    void import("@xterm/addon-webgl")
      .then(({ WebglAddon }) => {
        if (disposed) return
        try {
          const webgl = new WebglAddon()
          webgl.onContextLoss(() => webgl.dispose())
          xterm.loadAddon(webgl)
        } catch {
          // DOM renderer stays active.
        }
      })
      .catch(() => undefined)

    const gate = new SeqGate((data) => xterm.write(decodeBase64(data)))
    const offOutput = on<OutputEvent>("pane:output", (event) => {
      if (event.pane_id === paneID) gate.push(event.seq, event.data)
    })

    // Fit to the content box and tell the core, debounced with a trailing call.
    const sendResize = debounce((cols: number, rows: number) => bridge.paneResize(paneID, cols, rows), 50)
    const refit = () => {
      if (element.clientWidth === 0 || element.clientHeight === 0) return
      const size = fit.proposeDimensions()
      if (!size || size.cols < 2 || size.rows < 2) return
      if (size.cols !== xterm.cols || size.rows !== xterm.rows) xterm.resize(size.cols, size.rows)
      sendResize(size.cols, size.rows)
    }
    const observer = new ResizeObserver(() => refit())
    observer.observe(element)

    void bridge
      .paneAttach(paneID)
      .then((attach) => {
        if (disposed) return
        if (attach.cols >= 2 && attach.rows >= 2) xterm.resize(attach.cols, attach.rows)
        if (attach.preamble) xterm.write(attach.preamble)
        if (attach.replay) xterm.write(decodeBase64(attach.replay))
        gate.open(attach.seq)
        refit()
      })
      .catch(() => gate.open(0))

    // Pastes go to the core, which brackets them for agents and programs
    // that enabled bracketed paste, exactly like the TUI. xterm.js only
    // brackets when it saw DECSET 2004 itself, which a reattached agent sent
    // long ago, so zot would read each pasted line as Enter.
    const onPaste = (event: ClipboardEvent) => {
      const text = event.clipboardData?.getData("text/plain") ?? ""
      event.preventDefault()
      event.stopImmediatePropagation()
      if (text) pasteText(text)
    }
    const pasteText = (text: string) => {
      xterm.scrollToBottom()
      bridge.panePaste(paneID, normalizePaste(text))
    }
    element.addEventListener("paste", onPaste, true)

    const input = xterm.onData((data) => bridge.paneInput(paneID, data))
    const binary = xterm.onBinary((data) => bridge.paneInputBinary(paneID, encodeBinary(data)))
    xterm.attachCustomKeyEventHandler((event) => {
      // The prefix and native shortcuts are routed first.
      if (handleTerminalKey(event, keyName(event))) return false
      const override = overrideKey(event, kitty.current, IS_MAC)
      if (override !== undefined) {
        event.preventDefault()
        bridge.paneInput(paneID, override)
        return false
      }
      return true
    })

    const copy = xterm.onSelectionChange(() => {
      const selection = xterm.getSelection()
      if (selection && getView()?.auto_copy) {
        void bridge
          .clipboardWrite(selection)
          .then(() => copiedToast(selection))
          .catch(() => undefined)
      }
    })

    const unregister = registerTerminal(paneID, {
      focus: () => xterm.focus(),
      scrollPages: (pages) => xterm.scrollLines(Math.round(pages * xterm.rows)),
      scrollToBottom: () => {
        xterm.clearSelection()
        xterm.scrollToBottom()
      },
      copySelection: () => xterm.getSelection(),
      paste: (text) => pasteText(text),
    })

    return () => {
      disposed = true
      element.removeEventListener("paste", onPaste, true)
      unregister()
      observer.disconnect()
      offOutput()
      input.dispose()
      binary.dispose()
      copy.dispose()
      queries.dispose()
      xterm.dispose()
      term.current = null
    }
  }, [paneID])

  // Live theme and font updates without remounting.
  useEffect(() => {
    const handler = () => {
      const xterm = term.current
      const view = getView()
      if (!xterm || !view) return
      xterm.options.theme = xtermTheme(view.theme.colors)
      const prefs = view.prefs
      const family = prefs.font_family ? `${prefs.font_family}, ${MONO}` : MONO
      if (xterm.options.fontFamily !== family) xterm.options.fontFamily = family
      if (xterm.options.fontSize !== prefs.font_size) xterm.options.fontSize = prefs.font_size
      if (xterm.options.lineHeight !== prefs.line_height) xterm.options.lineHeight = prefs.line_height
      if (xterm.options.cursorBlink !== prefs.cursor_blink) xterm.options.cursorBlink = prefs.cursor_blink
      if (xterm.options.scrollback !== prefs.scrollback_lines) xterm.options.scrollback = prefs.scrollback_lines
    }
    return on("state", handler)
  }, [])

  useEffect(() => {
    if (focused) term.current?.focus()
  }, [focused])

  return <div ref={host} className={styles.terminal} data-mac={IS_MAC || undefined} />
}
