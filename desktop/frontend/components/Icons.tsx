"use client"

import styles from "./Icons.module.css"

// Logos of the built-in harnesses, served from public/agents. Custom
// harnesses and shells fall back to the prompt glyph.
const LOGOS = new Set(["claude", "codex", "zot", "pi"])

export function AgentIcon({ kind, size = 16 }: { kind: string; size?: number }) {
  if (!LOGOS.has(kind)) {
    return (
      <span className={styles.shell} style={{ width: size, height: size, fontSize: size * 0.62 }} aria-hidden>
        &gt;_
      </span>
    )
  }
  // Logos fill their whole square, so they look larger than the shell badge.
  // Render them smaller inside the same box to keep labels aligned.
  const logo = Math.round(size * 0.72)
  return (
    <span className={styles.logoBox} style={{ width: size, height: size }} aria-hidden>
      <img className={styles.logo} src={`/agents/${kind}.svg`} width={logo} height={logo} alt="" draggable={false} />
    </span>
  )
}

const PALETTE = ["#22c55e", "#f97316", "#3b82f6", "#a855f7", "#ec4899", "#14b8a6", "#eab308", "#ef4444", "#6366f1"]

/** Stable colored initial for a workspace. */
export function WorkspaceIcon({ name, size = 22 }: { name: string; size?: number }) {
  let hash = 0
  for (const char of name) hash = (hash * 31 + char.charCodeAt(0)) >>> 0
  const color = PALETTE[hash % PALETTE.length]
  return (
    <span className={styles.workspace} style={{ width: size, height: size, background: color, fontSize: size * 0.5 }} aria-hidden>
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  )
}

/** Line icons sharing one stroke style, so chrome buttons match. */
export function LineIcon({ name, size = 15 }: { name: "plus" | "search" | "close" | "settings" | "sidebar" | "chevron"; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      {name === "plus" ? <path d="M12 5v14M5 12h14" /> : null}
      {name === "search" ? (
        <>
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </>
      ) : null}
      {name === "close" ? <path d="M18 6 6 18M6 6l12 12" /> : null}
      {name === "chevron" ? <path d="m6 9 6 6 6-6" /> : null}
      {name === "sidebar" ? (
        <>
          <rect x="3" y="4" width="18" height="16" rx="3" />
          <path d="M9 4v16" />
        </>
      ) : null}
      {name === "settings" ? (
        <>
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </>
      ) : null}
    </svg>
  )
}
