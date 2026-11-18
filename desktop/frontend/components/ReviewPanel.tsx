"use client"

// Read-only Files and Changes panels, docked right of the panes like
// herdr's. Data comes from internal/review through the bridge; nothing here
// writes to disk or to git.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { bridge } from "@/lib/bridge"
import { useView } from "@/lib/store"
import type { ChangedFile, ChangesResult, DiffResult, FilePreview, FolderListing, Span } from "@/lib/types"
import { closePanel, type ReviewPanel as Panel } from "@/lib/ui"
import { LineIcon } from "./Icons"
import styles from "./ReviewPanel.module.css"

/** Refresh interval while a panel is open, in milliseconds. */
const REFRESH_MS = 5000

/** Renders highlighted spans; classes map to theme colors in the CSS. */
export function Code({ spans }: { spans: Span[] | null | undefined }): ReactNode {
  if (!spans || spans.length === 0) return " "
  return spans.map((span, index) =>
    span.c ? (
      <span key={index} className={styles[`hl_${span.c}`]}>
        {span.t}
      </span>
    ) : (
      span.t
    ),
  )
}

/** Splits "dir/sub/name" into its folder (with trailing slash) and name. */
export function splitPath(path: string): [string, string] {
  const slash = path.lastIndexOf("/")
  return slash < 0 ? ["", path] : [path.slice(0, slash + 1), path.slice(slash + 1)]
}

/** The folder key the panels follow: workspace, tab, and focused pane. */
function useFocusKey(): string {
  const view = useView()
  const workspace = view?.workspaces?.[view.selected]
  const tab = workspace?.tabs?.[workspace.active]
  return `${view?.selected}:${workspace?.path}:${tab?.focused_pane_id}`
}

/** Runs load now, whenever key changes, and every REFRESH_MS. */
function useRefresh(key: string, load: () => void): void {
  const loader = useRef(load)
  loader.current = load
  useEffect(() => {
    loader.current()
    const timer = setInterval(() => loader.current(), REFRESH_MS)
    return () => clearInterval(timer)
  }, [key])
}

function Header({ title, children, onBack }: { title: string; children?: ReactNode; onBack?: () => void }) {
  return (
    <div className={styles.header}>
      {onBack ? (
        <button className={styles.iconButton} title="Back" onClick={onBack}>
          <LineIcon name="back" size={14} />
        </button>
      ) : null}
      <span className={styles.title}>{title}</span>
      <div className={styles.headerInfo}>{children}</div>
      <button className={styles.iconButton} title="Close" onClick={closePanel}>
        <LineIcon name="close" size={14} />
      </button>
    </div>
  )
}

function markTone(mark: string | undefined): string | undefined {
  if (mark === "A") return "add"
  if (mark === "D" || mark === "U") return "del"
  if (mark === "M") return "mod"
  return undefined
}

/** Line-numbered, highlighted code for previews and diffs. */
function CodeView({ children, note }: { children: ReactNode; note?: string }) {
  return (
    <div className={styles.code}>
      {note ? <div className={styles.note}>{note}</div> : null}
      <div className={styles.codeInner}>{children}</div>
    </div>
  )
}

function FilesPanel() {
  const focusKey = useFocusKey()
  const [root, setRoot] = useState("")
  const [listings, setListings] = useState<Record<string, FolderListing>>({})
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [file, setFile] = useState<string | null>(null)
  const [preview, setPreview] = useState<FilePreview | null>(null)
  const openRef = useRef(open)
  openRef.current = open

  // A new folder starts closed and on the tree.
  useEffect(() => {
    setListings({})
    setOpen(new Set())
    setFile(null)
  }, [focusKey])

  const load = useCallback(() => {
    void bridge
      .reviewDir()
      .then(setRoot)
      .catch(() => setRoot(""))
    for (const folder of ["", ...openRef.current]) {
      void bridge
        .reviewList(folder)
        .then((listing) => setListings((old) => ({ ...old, [folder]: listing })))
        .catch(() => undefined)
    }
  }, [])
  useRefresh(focusKey, load)

  const toggle = (folder: string) => {
    const next = new Set(open)
    if (next.has(folder)) {
      for (const path of next) if (path === folder || path.startsWith(folder + "/")) next.delete(path)
    } else {
      next.add(folder)
      void bridge
        .reviewList(folder)
        .then((listing) => setListings((old) => ({ ...old, [folder]: listing })))
        .catch(() => undefined)
    }
    setOpen(next)
  }

  const show = (path: string) => {
    setFile(path)
    setPreview(null)
    void bridge
      .reviewPreview(path)
      .then(setPreview)
      .catch((error: unknown) => setPreview({ lines: null, note: String(error) }))
  }

  if (file) {
    return (
      <>
        <Header title="Files" onBack={() => setFile(null)}>
          <span className={styles.path} title={file}>
            {file}
          </span>
        </Header>
        {preview ? (
          <CodeView note={preview.note}>
            {(preview.lines ?? []).map((spans, index) => (
              <div key={index} className={styles.line}>
                <span className={styles.number}>{index + 1}</span>
                <span className={styles.text}>
                  <Code spans={spans} />
                </span>
              </div>
            ))}
          </CodeView>
        ) : (
          <div className={styles.empty}>Loading file…</div>
        )}
      </>
    )
  }

  const marks = listings[""]?.marks ?? {}
  const rows: ReactNode[] = []
  const walk = (folder: string, depth: number) => {
    for (const entry of listings[folder]?.entries ?? []) {
      const path = folder ? `${folder}/${entry.name}` : entry.name
      const expanded = open.has(path)
      const mark = marks[path]
      rows.push(
        <button
          key={path}
          className={styles.row}
          style={{ paddingLeft: 8 + depth * 16 }}
          onClick={() => (entry.dir ? toggle(path) : show(path))}
          title={path}
        >
          <span className={styles.chevron} data-open={expanded || undefined}>
            {entry.dir ? <LineIcon name="chevron" size={12} /> : null}
          </span>
          <span className={styles.entryIcon}>
            <LineIcon name={entry.dir ? "folder" : "file"} size={14} />
          </span>
          <span className={styles.name} data-tone={markTone(mark)}>
            {entry.name}
          </span>
          {mark && !entry.dir ? (
            <span className={styles.mark} data-tone={markTone(mark)}>
              {mark}
            </span>
          ) : null}
        </button>,
      )
      if (entry.dir && expanded) walk(path, depth + 1)
    }
  }
  walk("", 0)
  const top = listings[""]
  const limited = Object.values(listings).some((listing) => listing.limited)

  return (
    <>
      <Header title="Files">
        <span className={styles.path} title={root}>
          {root.split(/[\\/]/).pop()}
        </span>
      </Header>
      <div className={styles.list}>
        {rows}
        {!top ? <div className={styles.empty}>Loading files…</div> : null}
        {top?.error ? <div className={styles.empty}>{top.error}</div> : null}
        {top && !top.error && rows.length === 0 ? <div className={styles.empty}>No files</div> : null}
        {limited ? <div className={styles.hint}>Folders are limited to 500 entries</div> : null}
      </div>
    </>
  )
}

function ChangesPanel() {
  const focusKey = useFocusKey()
  const [changes, setChanges] = useState<ChangesResult | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [diff, setDiff] = useState<DiffResult | null>(null)
  const selectedRef = useRef(selected)
  selectedRef.current = selected
  const closedRef = useRef(false)

  useEffect(() => {
    setChanges(null)
    setSelected(null)
    setDiff(null)
    closedRef.current = false
  }, [focusKey])

  const loadDiff = useCallback((path: string) => {
    void bridge
      .reviewDiff(path)
      .then((result) => {
        if (selectedRef.current === path) setDiff(result)
      })
      .catch((error: unknown) => setDiff({ lines: null, note: String(error) }))
  }, [])

  const load = useCallback(() => {
    void bridge
      .reviewChanges()
      .then((result) => {
        setChanges(result)
        const files = result.files ?? []
        let current = selectedRef.current
        if (current && !files.some((file) => file.path === current)) current = null
        // The first file's diff shows until you pick another, like herdr.
        if (!current && !closedRef.current && files[0]) current = files[0].path
        setSelected(current)
        selectedRef.current = current
        if (current) loadDiff(current)
        else setDiff(null)
      })
      .catch((error: unknown) => setChanges({ files: null, error: String(error) }))
  }, [loadDiff])
  useRefresh(focusKey, load)

  const pick = (path: string) => {
    const next = selected === path ? null : path
    closedRef.current = next === null
    setSelected(next)
    selectedRef.current = next
    setDiff(null)
    if (next) loadDiff(next)
  }

  const files = changes?.files ?? []
  const add = files.reduce((sum, file) => sum + file.add, 0)
  const del = files.reduce((sum, file) => sum + file.del, 0)

  return (
    <>
      <Header title="Changes">
        <span className={styles.badge}>
          {files.length} {files.length === 1 ? "file" : "files"}
        </span>
        <span className={styles.add}>+{add}</span>
        <span className={styles.del}>−{del}</span>
      </Header>
      <div className={styles.changeList}>
        {files.map((file: ChangedFile) => {
          const [folder, name] = splitPath(file.path)
          return (
            <button
              key={file.path}
              className={styles.row}
              data-selected={selected === file.path || undefined}
              onClick={() => pick(file.path)}
              title={file.path}
            >
              <span className={styles.mark} data-tone={markTone(file.status)}>
                {file.status}
              </span>
              <span className={styles.changePath}>
                <span className={styles.folder}>{folder}</span>
                <span className={styles.fileName}>{name}</span>
              </span>
              {file.add > 0 ? <span className={styles.add}>+{file.add}</span> : null}
              {file.del > 0 ? <span className={styles.del}>−{file.del}</span> : null}
            </button>
          )
        })}
        {!changes ? <div className={styles.empty}>Loading changes…</div> : null}
        {changes?.error ? <div className={styles.empty}>{changes.error}</div> : null}
        {changes && !changes.error && files.length === 0 ? <div className={styles.empty}>No uncommitted changes</div> : null}
        {changes?.limited ? <div className={styles.hint}>Showing the first 1,000 changed files</div> : null}
      </div>
      {selected ? (
        diff ? (
          <CodeView note={diff.note}>
            {(diff.lines ?? []).map((line, index) =>
              line.k === "@" ? (
                <div key={index} className={styles.hunk}>
                  <Code spans={line.s} />
                </div>
              ) : (
                <div key={index} className={styles.line} data-kind={line.k === "+" ? "add" : line.k === "-" ? "del" : undefined}>
                  <span className={styles.number}>{line.n || ""}</span>
                  <span className={styles.sign}>{line.k === "+" || line.k === "-" ? line.k : ""}</span>
                  <span className={styles.text}>
                    <Code spans={line.s} />
                  </span>
                </div>
              ),
            )}
          </CodeView>
        ) : (
          <div className={styles.empty}>Loading diff…</div>
        )
      ) : files.length > 0 ? (
        <div className={styles.empty}>Select a file to view its changes</div>
      ) : null}
    </>
  )
}

export default function ReviewPanel({ panel }: { panel: Panel }) {
  return <aside className={styles.panel}>{panel === "files" ? <FilesPanel /> : <ChangesPanel />}</aside>
}
