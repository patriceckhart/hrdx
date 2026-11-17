"use client"

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { bridge, errorMessage } from "@/lib/bridge"
import type { Prefs, SettingsPatch, SettingsView } from "@/lib/types"
import { closeModal } from "@/lib/ui"
import { Dialog } from "./Dialog"
import { AgentIcon } from "./Icons"
import styles from "./Overlay.module.css"

const SPLITS: [string, string][] = [
  ["", "Right"],
  ["down", "Below"],
  ["auto", "Auto (longer side)"],
]

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className={styles.group}>
      <h3>{title}</h3>
      <div className={styles.card}>{children}</div>
    </section>
  )
}

/** One row: label and optional description left, control right. */
function Setting({ label, description, children }: { label: ReactNode; description?: ReactNode; children: ReactNode }) {
  return (
    <label className={styles.setting}>
      <span className={styles.settingText}>
        {label}
        {description ? <small>{description}</small> : null}
      </span>
      <span className={styles.settingControl}>{children}</span>
    </label>
  )
}

function Switch({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  return <input type="checkbox" role="switch" className={styles.switch} checked={checked} onChange={(event) => onChange(event.target.checked)} />
}

export default function SettingsDialog() {
  const [settings, setSettings] = useState<SettingsView | null>(null)
  const [error, setError] = useState("")
  // Depth restored when the switch is turned back on, like the TUI.
  const lastDepth = useRef(1)

  const load = useCallback(() => {
    void bridge
      .settingsGet()
      .then((next) => {
        if (next.subrepo_depth > 0) lastDepth.current = next.subrepo_depth
        setSettings(next)
      })
      .catch((failure: unknown) => setError(errorMessage(failure)))
  }, [])
  useEffect(load, [load])

  const apply = async (patch: SettingsPatch) => {
    setError("")
    try {
      await bridge.settingsSet(patch)
      load()
    } catch (failure) {
      setError(errorMessage(failure))
      load()
    }
  }
  const setPrefs = (patch: Partial<Prefs>) => {
    if (settings) void apply({ prefs: { ...settings.prefs, ...patch } })
  }

  if (!settings) return <Dialog title="Settings">{error ? <div className={styles.error}>{error}</div> : null}</Dialog>
  const prefs = settings.prefs
  const agents = settings.agents ?? []

  return (
    <Dialog title="Settings" wide>
      <div className={styles.settings}>
        <Group title="Agents">
          {agents.map((agent) => (
            <Setting
              key={agent.kind}
              label={
                <span className={styles.agentName}>
                  <AgentIcon kind={agent.kind} size={18} />
                  {agent.kind}
                  {agent.custom ? <span className={styles.hint}>custom</span> : null}
                  {agent.installed ? null : <span className={styles.hint}>not installed</span>}
                </span>
              }
            >
              <Switch checked={agent.enabled} onChange={(enabled) => void apply({ agent_enabled: { [agent.kind]: enabled } })} />
            </Setting>
          ))}
          <Setting label="Default agent" description="Used for new panes and workspaces.">
            <select value={settings.default_agent} onChange={(event) => void apply({ default_agent: event.target.value })}>
              {agents
                .filter((agent) => agent.enabled)
                .map((agent) => (
                  <option key={agent.kind}>{agent.kind}</option>
                ))}
            </select>
          </Setting>
        </Group>

        <Group title="Notifications">
          <Setting label="Sound" description="Play a sound when an agent finishes.">
            <Switch checked={settings.sound} onChange={(sound) => void apply({ sound })} />
          </Setting>
          <Setting label="Sound effect">
            <select value={settings.sound_kind} onChange={(event) => void apply({ sound_kind: event.target.value })}>
              {(settings.sound_kinds ?? []).map((kind) => (
                <option key={kind}>{kind}</option>
              ))}
            </select>
            <button
              type="button"
              className={styles.button}
              onClick={(event) => {
                event.preventDefault()
                void bridge.soundPreview(settings.sound_kind).catch(() => undefined)
              }}
            >
              Preview
            </button>
          </Setting>
          <Setting
            label="System notifications"
            description={settings.notify_supported ? "Show a banner and bounce the Dock icon." : "Not available on this platform yet."}
          >
            <Switch checked={settings.notify} onChange={(notify) => void apply({ notify })} />
          </Setting>
        </Group>

        <Group title="Appearance">
          <Setting label="Theme">
            <select value={settings.theme} onChange={(event) => void apply({ theme: event.target.value })}>
              {(settings.themes ?? []).map((name) => (
                <option key={name}>{name}</option>
              ))}
            </select>
          </Setting>
          <Setting label="Font family" description="Leave empty for the system monospace font.">
            <input
              className={styles.input}
              placeholder="system monospace"
              defaultValue={prefs.font_family}
              onBlur={(event) => event.target.value !== prefs.font_family && setPrefs({ font_family: event.target.value.trim() })}
            />
          </Setting>
          <Setting label="Font size">
            <input
              type="number"
              min={6}
              max={72}
              className={`${styles.input} ${styles.narrow}`}
              defaultValue={prefs.font_size}
              onBlur={(event) => setPrefs({ font_size: Number(event.target.value) })}
            />
          </Setting>
          <Setting label="Line height">
            <input
              type="number"
              step={0.05}
              min={0.8}
              max={3}
              className={`${styles.input} ${styles.narrow}`}
              defaultValue={prefs.line_height}
              onBlur={(event) => setPrefs({ line_height: Number(event.target.value) })}
            />
          </Setting>
          <Setting label="Blinking cursor">
            <Switch checked={prefs.cursor_blink} onChange={(cursor_blink) => setPrefs({ cursor_blink })} />
          </Setting>
        </Group>

        <Group title="Behavior">
          <Setting label="Copy on select" description="Copy selections to the clipboard automatically.">
            <Switch checked={settings.auto_copy} onChange={(auto_copy) => void apply({ auto_copy })} />
          </Setting>
          <Setting label="Default split">
            <select value={settings.split_direction} onChange={(event) => void apply({ split_direction: event.target.value })}>
              {SPLITS.map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </Setting>
          <Setting label="Workspace folding" description="Fold workspaces in the sidebar.">
            <Switch checked={settings.workspace_folding} onChange={(workspace_folding) => void apply({ workspace_folding })} />
          </Setting>
          <Setting label="Nested repositories" description="List git repositories inside each workspace with branch and status.">
            <Switch
              checked={settings.subrepo_depth > 0}
              onChange={(enabled) => void apply({ subrepo_depth: enabled ? Math.max(1, lastDepth.current) : 0 })}
            />
          </Setting>
          {settings.subrepo_depth > 0 ? (
            <Setting label="Search depth" description="Folder levels below the workspace to search.">
              <select
                value={settings.subrepo_depth}
                onChange={(event) => {
                  lastDepth.current = Number(event.target.value)
                  void apply({ subrepo_depth: lastDepth.current })
                }}
              >
                {[1, 2, 3].map((depth) => (
                  <option key={depth} value={depth}>
                    {depth === 1 ? "1 level" : `${depth} levels`}
                  </option>
                ))}
              </select>
            </Setting>
          ) : null}
          <Setting label="Scrollback lines">
            <input
              type="number"
              min={100}
              step={1000}
              className={`${styles.input} ${styles.narrow}`}
              defaultValue={prefs.scrollback_lines}
              onBlur={(event) => setPrefs({ scrollback_lines: Number(event.target.value) })}
            />
          </Setting>
        </Group>
      </div>
      {error ? <div className={styles.error}>{error}</div> : null}
      <div className={styles.actions}>
        <button className={styles.primary} onClick={closeModal}>
          Done
        </button>
      </div>
    </Dialog>
  )
}
