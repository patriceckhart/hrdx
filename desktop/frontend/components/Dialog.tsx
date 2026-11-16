"use client"

import type { KeyboardEvent, ReactNode } from "react"
import { closeModal } from "@/lib/ui"
import styles from "./Overlay.module.css"

/** Shared modal chrome. Keys inside never reach terminals or the prefix. */
export function Dialog({
  title,
  children,
  wide,
  onKeyDown,
}: {
  title: string
  children: ReactNode
  wide?: boolean
  onKeyDown?: (event: KeyboardEvent) => void
}) {
  return (
    <div className={styles.backdrop} onMouseDown={(event) => event.target === event.currentTarget && closeModal()}>
      <div
        className={styles.dialog}
        data-wide={wide || undefined}
        role="dialog"
        aria-label={title}
        onKeyDown={(event) => {
          event.stopPropagation()
          if (event.key === "Escape") {
            event.preventDefault()
            closeModal()
            return
          }
          onKeyDown?.(event)
        }}
      >
        <div className={styles.dialogTitle}>{title}</div>
        {children}
      </div>
    </div>
  )
}
