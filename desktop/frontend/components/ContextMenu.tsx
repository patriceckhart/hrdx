"use client"

import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { closeMenu, useUI } from "@/lib/ui"
import styles from "./Overlay.module.css"

/** Renders the open context menu; consumes only its own keys. */
export default function ContextMenu() {
  const { menu } = useUI()
  const box = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState({ x: 0, y: 0 })
  const [active, setActive] = useState(0)

  useLayoutEffect(() => {
    if (!menu || !box.current) return
    const rect = box.current.getBoundingClientRect()
    setPosition({
      x: Math.min(menu.x, window.innerWidth - rect.width - 8),
      y: Math.min(menu.y, window.innerHeight - rect.height - 8),
    })
    setActive(0)
    box.current.focus()
  }, [menu])

  useEffect(() => {
    if (!menu) return
    const dismiss = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as Node)) closeMenu()
    }
    window.addEventListener("mousedown", dismiss, true)
    window.addEventListener("blur", closeMenu)
    return () => {
      window.removeEventListener("mousedown", dismiss, true)
      window.removeEventListener("blur", closeMenu)
    }
  }, [menu])

  if (!menu) return null

  const run = (index: number) => {
    const item = menu.items[index]
    closeMenu()
    if (item) void item.run()
  }

  return (
    <div
      ref={box}
      tabIndex={-1}
      className={styles.menu}
      style={{ left: position.x, top: position.y }}
      onKeyDown={(event) => {
        event.stopPropagation()
        if (event.key === "Escape") closeMenu()
        else if (event.key === "ArrowDown") setActive((active + 1) % menu.items.length)
        else if (event.key === "ArrowUp") setActive((active - 1 + menu.items.length) % menu.items.length)
        else if (event.key === "Enter") run(active)
        else return
        event.preventDefault()
      }}
    >
      {menu.items.map((item, index) => (
        <button
          key={`${index}:${item.label}`}
          className={styles.menuItem}
          data-active={index === active || undefined}
          data-danger={item.danger || undefined}
          onMouseEnter={() => setActive(index)}
          onClick={() => run(index)}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}
