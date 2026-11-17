// Plain-data mirrors of desktop/internal/core view types. The JSON shape is
// the contract; keep these in sync with view.go.

export interface NodeView {
  pane_id?: number
  vertical?: boolean
  ratio?: number
  a?: NodeView
  b?: NodeView
}

export interface PaneView {
  pane_id: number
  name: string
  kind: string
  title?: string
  running: boolean
  starting?: boolean
  busy: boolean
  attention?: boolean
  unseen?: boolean
  failure?: string
  kitty_keys?: boolean
  anchor?: string
  width_pct?: number
  height_pct?: number
  agent?: string
}

export interface TabView {
  name?: string
  focused_pane_id: number
  layout: NodeView | null
  panes: PaneView[] | null
  floats?: PaneView[] | null
}

export interface WorkspaceView {
  group_path?: string[]
  name: string
  path: string
  branch?: string
  folded?: boolean
  active: number
  tabs: TabView[] | null
  subrepos?: SubrepoView[]
  hidden_subrepos?: number
}

/** A git repository nested below a workspace. */
export interface SubrepoView {
  path: string
  /** Slash-separated path relative to the workspace. */
  rel: string
  branch?: string
  ahead?: number
  behind?: number
  changed?: number
  conflicts?: number
}

export interface AgentView {
  kind: string
  installed: boolean
  enabled: boolean
  custom?: boolean
}

export interface MenuRegistration {
  target: "pane" | "tab" | "sidebar"
  label: string
  action_id: string
}

export interface Prefs {
  window?: { x: number; y: number; width: number; height: number; maximized: boolean }
  font_family: string
  font_size: number
  line_height: number
  cursor_blink: boolean
  scrollback_lines: number
  sidebar_width: number
}

export interface View {
  version: string
  workspaces: WorkspaceView[] | null
  selected: number
  sidebar_collapsed: boolean
  sidebar_width: number
  split_direction: string
  workspace_folding: boolean
  default_agent: string
  agents: AgentView[] | null
  theme: { name: string; colors: Record<string, string> }
  keymap: Record<string, string>
  prefix: string
  nav_keys: Record<string, string> | null
  persisting: boolean
  auto_copy: boolean
  menus: MenuRegistration[] | null
  prefs: Prefs
  problems?: string[]
  blocked?: string
}

export interface SettingsView {
  agents: AgentView[] | null
  default_agent: string
  sound: boolean
  sound_kind: string
  sound_kinds: string[] | null
  notify: boolean
  notify_supported: boolean
  theme: string
  themes: string[] | null
  auto_copy: boolean
  split_direction: string
  workspace_folding: boolean
  subrepo_depth: number
  prefs: Prefs
}

export interface SettingsPatch {
  agent_enabled?: Record<string, boolean>
  default_agent?: string
  sound?: boolean
  sound_kind?: string
  notify?: boolean
  theme?: string
  auto_copy?: boolean
  split_direction?: string
  workspace_folding?: boolean
  sidebar_collapsed?: boolean
  subrepo_depth?: number
  prefs?: Prefs
}

export interface AttachResult {
  cols: number
  rows: number
  seq: number
  preamble: string
  replay: string
  running: boolean
}

export interface UpdateInfo {
  current: string
  latest: string
  available: boolean
  url: string
}

export interface OutputEvent {
  pane_id: number
  seq: number
  data: string
}
