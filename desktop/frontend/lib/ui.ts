// Transient UI state: modals, menus, toasts, prefix indicator, and terminal
// registry for focus and scrolling. Nothing here is model state.

import { useSyncExternalStore } from "react"
import { getView } from "./store"

export type Modal =
  | { kind: "picker"; title: string; onPick: (kind: string) => void }
  | { kind: "workspace" }
  | { kind: "finder" }
  | { kind: "settings" }
  | { kind: "rename"; title: string; initial: string; allowEmpty?: boolean; onSubmit: (name: string) => Promise<void> }
  | { kind: "confirm"; title: string; message: string; action: string; onConfirm: () => Promise<void> }

export interface MenuItem {
  label: string
  run: () => void | Promise<void>
  danger?: boolean
}

export interface MenuState {
  x: number
  y: number
  items: MenuItem[]
}

/** The read-only side panel next to the panes. */
export type ReviewPanel = "files" | "changes"

export interface UIState {
  modal: Modal | null
  menu: MenuState | null
  prefix: boolean
  toast: string | null
  panel: ReviewPanel | null
}

let state: UIState = { modal: null, menu: null, prefix: false, toast: null, panel: null }
const listeners = new Set<() => void>()

function update(patch: Partial<UIState>): void {
  state = { ...state, ...patch }
  for (const listener of listeners) listener()
}

export function useUI(): UIState {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => state,
    () => state,
  )
}

export function uiState(): UIState {
  return state
}

export function openModal(modal: Modal): void {
  update({ modal, menu: null })
}

export function closeModal(): void {
  update({ modal: null })
  refocusTerminal()
}

export function openMenu(menu: MenuState): void {
  update({ menu })
}

export function closeMenu(): void {
  if (state.menu) {
    update({ menu: null })
    refocusTerminal()
  }
}

/** Opens a review panel, or closes it when it is already open. */
export function togglePanel(panel: ReviewPanel): void {
  update({ panel: state.panel === panel ? null : panel })
  if (state.panel === null) refocusTerminal()
}

export function closePanel(): void {
  if (state.panel) {
    update({ panel: null })
    refocusTerminal()
  }
}

export function setPrefix(active: boolean): void {
  if (state.prefix !== active) update({ prefix: active })
}

let toastTimer: ReturnType<typeof setTimeout> | undefined
export function toast(message: string, durationMs = 3500): void {
  update({ toast: message })
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => update({ toast: null }), durationMs)
}

/** Confirms a clipboard copy briefly, like the TUI's "copied N chars". */
export function copiedToast(text: string): void {
  toast(`copied ${[...text].length} chars`, 2000)
}

export function modalOpen(): boolean {
  return state.modal !== null || state.menu !== null || Boolean(getView()?.blocked)
}

// Terminal registry: mounted terminals register focus and scroll handles so
// global key handling can reach the focused one without React state.

export interface TerminalHandle {
  focus: () => void
  scrollPages: (pages: number) => void
  scrollToBottom: () => void
  copySelection: () => string
  paste: (text: string) => void
}

const terminals = new Map<number, TerminalHandle>()
let focusedID = 0

export function registerTerminal(id: number, handle: TerminalHandle): () => void {
  terminals.set(id, handle)
  return () => {
    if (terminals.get(id) === handle) terminals.delete(id)
  }
}

export function setFocusedTerminal(id: number): void {
  focusedID = id
}

export function terminalByID(id: number): TerminalHandle | undefined {
  return terminals.get(id)
}

export function focusedTerminal(): TerminalHandle | undefined {
  return terminals.get(focusedID)
}

export function refocusTerminal(): void {
  if (modalOpen()) return
  requestAnimationFrame(() => focusedTerminal()?.focus())
}
