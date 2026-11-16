"use client"

import { useEffect, useState } from "react"
import { available, bridge, errorMessage } from "@/lib/bridge"
import type { UpdateInfo } from "@/lib/types"
import styles from "./App.module.css"

// GitHub allows 60 unauthenticated API calls an hour, so an hourly check
// plus throttled focus checks stays far below the limit.
const recheckMs = 60 * 60 * 1000
const focusThrottleMs = 10 * 60 * 1000

/**
 * Offers a newer release with a one-click install. Checks at launch, every
 * hour, and when the window regains focus (at most every ten minutes).
 */
export default function UpdateNotice() {
  const [info, setInfo] = useState<UpdateInfo | null>(null)
  const [dismissed, setDismissed] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!available()) return
    let last = 0
    const check = () => {
      last = Date.now()
      void bridge
        .updateCheck()
        .then(setInfo)
        .catch(() => undefined)
    }
    const onFocus = () => {
      if (Date.now() - last >= focusThrottleMs) check()
    }
    check()
    const timer = setInterval(check, recheckMs)
    window.addEventListener("focus", onFocus)
    return () => {
      clearInterval(timer)
      window.removeEventListener("focus", onFocus)
    }
  }, [])

  if (!info?.available || dismissed === info.latest) return null

  const install = () => {
    setBusy(true)
    setError("")
    // On success the app quits and the new version starts.
    bridge.updateInstall().catch((reason: unknown) => {
      setBusy(false)
      setError(errorMessage(reason))
    })
  }

  return (
    <div className={styles.update} role="status">
      <div>
        <strong>hrdx {info.latest} is available.</strong> You have {info.current}. Sessions keep running across the restart.
        {error ? <div className={styles.updateError}>Update failed: {error}</div> : null}
      </div>
      <div className={styles.updateActions}>
        <button onClick={() => void bridge.openExternal(info.url).catch(() => undefined)} disabled={busy}>
          Release notes
        </button>
        <button onClick={() => setDismissed(info.latest)} disabled={busy}>
          Later
        </button>
        <button className={styles.primary} onClick={install} disabled={busy}>
          {busy ? "Updating..." : "Update now and restart"}
        </button>
      </div>
    </div>
  )
}
