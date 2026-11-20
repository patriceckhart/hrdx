"use client"

import { useEffect } from "react"
import { bridge } from "@/lib/bridge"
import { keyName, PrefixMachine } from "@/lib/keys"
import { quit, report, runAction, splitPicker } from "@/lib/actions"
import { focusedPane, getView } from "@/lib/store"
import { copiedToast, focusedTerminal, modalOpen, openModal, setPrefix } from "@/lib/ui"

const IS_MAC = typeof navigator !== "undefined" && /Mac/.test(navigator.platform)

const machine = new PrefixMachine("ctrl+b", {})

/** Keeps the prefix machine in sync with the view's keymap. */
export function configureKeys(prefix: string, keymap: Record<string, string>): void {
  machine.configure(prefix, keymap)
}

/**
 * Native shortcuts layered on top of the prefix. On macOS they use cmd. On
 * Linux and Windows they need ctrl+shift so plain control keys stay
 * terminal input.
 */
function nativeShortcut(event: KeyboardEvent): boolean {
  const primary = IS_MAC ? event.metaKey && !event.ctrlKey : event.ctrlKey && event.shiftKey && !event.metaKey
  if (!primary || event.altKey) return false
  const key = event.key.toLowerCase()
  switch (key) {
    case "q":
      quit()
      return true
    case ",":
    case "<": // ctrl+shift+, reports "<" on US layouts
      openModal({ kind: "settings" })
      return true
    case "w": {
      const pane = focusedPane(getView())
      if (pane) report(bridge.paneClose(pane.pane_id))
      return true
    }
    case "t":
      splitPicker("tab")
      return true
    case "k":
      openModal({ kind: "finder" })
      return true
    case "c": {
      // Text selected in the Files or Changes panel wins over the terminal.
      const text = selectedPanelText() || focusedTerminal()?.copySelection()
      if (text) {
        void bridge
          .clipboardWrite(text)
          .then(() => copiedToast(text))
          .catch(() => undefined)
      }
      return true
    }
    case "v":
      void bridge
        .clipboardRead()
        .then((text) => {
          if (text) focusedTerminal()?.paste(text)
        })
        .catch(() => undefined)
      return true
  }
  return false
}

/** The text selected inside a review panel's code view, if any. */
export function selectedPanelText(): string {
  const selection = window.getSelection()
  if (!selection || selection.isCollapsed) return ""
  const anchor = selection.anchorNode
  const element = anchor instanceof Element ? anchor : anchor?.parentElement
  return element?.closest("[data-selectable]") ? selection.toString() : ""
}

/**
 * Routes one keydown from a terminal. Returns true when hrdx consumed it, in
 * which case xterm.js must not encode it. Escape and plain control keys are
 * never reserved: they only act as the configured prefix.
 */
export function handleTerminalKey(event: KeyboardEvent, name: string): boolean {
  if (event.type !== "keydown") return machine.active
  if (modalOpen()) return false
  if (event.isComposing) return false
  if (!machine.active && nativeShortcut(event)) {
    event.preventDefault()
    return true
  }
  const result = machine.handle(name)
  switch (result.kind) {
    case "pass":
      return false
    case "enter":
      event.preventDefault()
      setPrefix(true)
      return true
    case "cancel":
      event.preventDefault()
      setPrefix(false)
      return true
    case "action":
      event.preventDefault()
      setPrefix(result.stay)
      runAction(result.action)
      return true
  }
}

/**
 * Handles keys when no terminal has focus (empty workspace, sidebar clicks):
 * the prefix and native shortcuts still work.
 */
export default function KeyRouter() {
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target?.closest(".xterm")) return // the terminal routes its own keys
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.tagName === "SELECT")) return
      if (handleTerminalKey(event, keyName(event))) event.stopPropagation()
    }
    window.addEventListener("keydown", listener)
    const focus = () => bridge.windowFocus(true)
    const blur = () => bridge.windowFocus(false)
    window.addEventListener("focus", focus)
    window.addEventListener("blur", blur)
    return () => {
      window.removeEventListener("keydown", listener)
      window.removeEventListener("focus", focus)
      window.removeEventListener("blur", blur)
    }
  }, [])
  return null
}
