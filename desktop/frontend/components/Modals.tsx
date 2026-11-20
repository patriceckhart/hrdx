"use client"

import { useEffect, useRef, useState, type KeyboardEvent } from "react"
import { bridge, errorMessage } from "@/lib/bridge"
import { pickerKinds, report } from "@/lib/actions"
import { getView, useView } from "@/lib/store"
import { closeModal, toast, useUI, type Modal } from "@/lib/ui"
import { Dialog } from "./Dialog"
import SettingsDialog from "./SettingsDialog"
import styles from "./Overlay.module.css"

/** Arrow and navigate-key selection shared by list modals. */
function useListKeys(count: number, onChoose: (index: number) => void) {
  const [active, setActive] = useState(0)
  useEffect(() => setActive((current) => Math.min(current, Math.max(0, count - 1))), [count])
  const nav = getView()?.nav_keys ?? {}
  const handle = (event: KeyboardEvent) => {
    const name = event.key
    const action = nav[name]
    if (name === "ArrowDown" || action === "navigate-down" || (event.ctrlKey && name === "n")) {
      setActive((active + 1) % Math.max(1, count))
    } else if (name === "ArrowUp" || action === "navigate-up" || (event.ctrlKey && name === "p")) {
      setActive((active - 1 + count) % Math.max(1, count))
    } else if (name === "Enter") {
      if (count > 0) onChoose(active)
    } else {
      return
    }
    event.preventDefault()
  }
  return { active, setActive, handle }
}

function Picker({ modal }: { modal: Extract<Modal, { kind: "picker" }> }) {
  const kinds = pickerKinds()
  const box = useRef<HTMLDivElement>(null)
  const { active, setActive, handle } = useListKeys(kinds.length, (index) => modal.onPick(kinds[index]!))
  useEffect(() => box.current?.focus(), [])
  return (
    <Dialog title={modal.title} onKeyDown={handle}>
      <div ref={box} tabIndex={-1} className={styles.list}>
        {kinds.map((kind, index) => (
          <button
            key={kind}
            className={styles.listItem}
            data-active={index === active || undefined}
            onMouseEnter={() => setActive(index)}
            onClick={() => modal.onPick(kind)}
          >
            {kind}
            {kind === getView()?.default_agent ? <span className={styles.hint}>default</span> : null}
          </button>
        ))}
      </div>
    </Dialog>
  )
}

function Finder() {
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<{ label: string; pane_id: number }[]>([])
  useEffect(() => {
    let current = true
    void bridge
      .find(query)
      .then((found) => current && setResults(found ?? []))
      .catch(() => undefined)
    return () => {
      current = false
    }
  }, [query])
  const jump = (index: number) => {
    const target = results[index]
    if (!target) return
    closeModal()
    report(bridge.paneFocus(target.pane_id))
  }
  const { active, setActive, handle } = useListKeys(Math.min(results.length, 12), jump)
  return (
    <Dialog title="Find" wide onKeyDown={handle}>
      <input
        autoFocus
        className={styles.input}
        placeholder="group, workspace, tab, or pane"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <div className={styles.list}>
        {results.slice(0, 12).map((result, index) => (
          <button
            key={result.pane_id}
            className={styles.listItem}
            data-active={index === active || undefined}
            onMouseEnter={() => setActive(index)}
            onClick={() => jump(index)}
          >
            {result.label}
          </button>
        ))}
        {results.length === 0 ? <div className={styles.empty}>No matches</div> : null}
      </div>
    </Dialog>
  )
}

function WorkspacePrompt() {
  const view = useView()
  const [path, setPath] = useState("")
  const [agent, setAgent] = useState(view?.default_agent ?? "zot")
  const [args, setArgs] = useState("")
  const [completions, setCompletions] = useState<string[]>([])
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let current = true
    void bridge
      .completePath(path)
      .then((found) => current && setCompletions(found ?? []))
      .catch(() => undefined)
    return () => {
      current = false
    }
  }, [path])

  const submit = async (value = path) => {
    if (!value.trim()) return
    setBusy(true)
    setError("")
    try {
      // Shells take no agent arguments.
      if (agent !== "shell" && args.trim()) await bridge.workspaceCreateWithArgs(value.trim(), agent, args)
      else await bridge.workspaceCreate(value.trim(), agent)
      closeModal()
    } catch (failure) {
      setError(errorMessage(failure))
    } finally {
      setBusy(false)
    }
  }

  const browse = async () => {
    try {
      const picked = await bridge.workspacePickDirectory()
      if (picked) await submit(picked)
    } catch (failure) {
      setError(errorMessage(failure))
    }
  }

  return (
    <Dialog
      title="New workspace"
      wide
      onKeyDown={(event) => {
        // Tab completes directories only while typing the directory.
        if (event.key === "Tab" && completions.length > 0 && event.target === input.current) {
          event.preventDefault()
          const next = completions.length === 1 ? completions[0]! : commonPrefix(completions)
          setPath(completions.length === 1 ? `${next}/` : next.length > path.length ? next : completions[0]!)
          input.current?.focus()
        } else if (event.key === "Enter") {
          event.preventDefault()
          void submit()
        }
      }}
    >
      <div className={styles.row}>
        <input
          ref={input}
          autoFocus
          className={styles.input}
          placeholder="directory (e.g. ~/Developer/api)"
          value={path}
          onChange={(event) => setPath(event.target.value)}
        />
        <button className={styles.button} onClick={() => void browse()}>
          Browse
        </button>
      </div>
      <div className={styles.completions}>
        {completions.map((completion) => (
          <button key={completion} className={styles.chip} onClick={() => setPath(`${completion}/`)}>
            {completion}
          </button>
        ))}
      </div>
      <label className={styles.field}>
        First pane
        <select value={agent} onChange={(event) => setAgent(event.target.value)}>
          {pickerKinds().map((kind) => (
            <option key={kind}>{kind}</option>
          ))}
        </select>
      </label>
      {agent !== "shell" ? (
        <label className={styles.field}>
          Arguments
          <input
            className={styles.input}
            placeholder="optional, e.g. --codemode"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            value={args}
            onChange={(event) => setArgs(event.target.value)}
          />
        </label>
      ) : null}
      {error ? <div className={styles.error}>{error}</div> : null}
      <div className={styles.actions}>
        <button className={styles.button} onClick={closeModal}>
          Cancel
        </button>
        <button className={styles.primary} disabled={busy || !path.trim()} onClick={() => void submit()}>
          Open
        </button>
      </div>
    </Dialog>
  )
}

function commonPrefix(values: string[]): string {
  if (values.length === 0) return ""
  let prefix = values[0]!
  for (const value of values.slice(1)) {
    while (!value.startsWith(prefix)) prefix = prefix.slice(0, -1)
  }
  return prefix
}

function Rename({ modal }: { modal: Extract<Modal, { kind: "rename" }> }) {
  const [value, setValue] = useState(modal.initial)
  const [error, setError] = useState("")
  const submit = async () => {
    if (!modal.allowEmpty && !value.trim()) return
    try {
      await modal.onSubmit(value)
      closeModal()
    } catch (failure) {
      setError(errorMessage(failure))
    }
  }
  return (
    <Dialog
      title={modal.title}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault()
          void submit()
        }
      }}
    >
      <input autoFocus className={styles.input} value={value} onFocus={(event) => event.target.select()} onChange={(event) => setValue(event.target.value)} />
      {error ? <div className={styles.error}>{error}</div> : null}
      <div className={styles.actions}>
        <button className={styles.button} onClick={closeModal}>
          Cancel
        </button>
        <button className={styles.primary} onClick={() => void submit()}>
          Save
        </button>
      </div>
    </Dialog>
  )
}

function Confirm({ modal }: { modal: Extract<Modal, { kind: "confirm" }> }) {
  const confirm = () => {
    closeModal()
    report(modal.onConfirm())
  }
  return (
    <Dialog
      title={modal.title}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault()
          confirm()
        }
      }}
    >
      <p className={styles.message}>{modal.message}</p>
      <div className={styles.actions}>
        <button className={styles.button} onClick={closeModal}>
          Cancel
        </button>
        <button autoFocus className={styles.danger} onClick={confirm}>
          {modal.action}
        </button>
      </div>
    </Dialog>
  )
}

/** Shown when another hrdx owns the session holder. Never writes state. */
function Blocked({ message }: { message: string }) {
  const [busy, setBusy] = useState(false)
  const retry = async () => {
    setBusy(true)
    try {
      await bridge.retryHolder()
    } catch (failure) {
      toast(errorMessage(failure))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className={styles.backdrop}>
      <div className={styles.dialog} role="alertdialog" aria-label="hrdx is busy">
        <div className={styles.dialogTitle}>hrdx is busy</div>
        <p className={styles.message}>{message}</p>
        <div className={styles.actions}>
          <button className={styles.button} onClick={() => void bridge.quit()}>
            Quit
          </button>
          <button autoFocus className={styles.primary} disabled={busy} onClick={() => void retry()}>
            Retry
          </button>
        </div>
      </div>
    </div>
  )
}

export default function Modals() {
  const { modal } = useUI()
  const blocked = useView()?.blocked
  if (blocked) return <Blocked message={blocked} />
  if (!modal) return null
  switch (modal.kind) {
    case "picker":
      return <Picker modal={modal} />
    case "finder":
      return <Finder />
    case "workspace":
      return <WorkspacePrompt />
    case "rename":
      return <Rename modal={modal} />
    case "confirm":
      return <Confirm modal={modal} />
    case "settings":
      return <SettingsDialog />
  }
}
