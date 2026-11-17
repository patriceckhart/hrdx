export namespace api {
	
	export class MenuRegister {
	    target: string;
	    label: string;
	    action_id: string;
	
	    static createFrom(source: any = {}) {
	        return new MenuRegister(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.label = source["label"];
	        this.action_id = source["action_id"];
	    }
	}

}

export namespace bridge {
	
	export class PaneAttachResult {
	    cols: number;
	    rows: number;
	    seq: number;
	    preamble: string;
	    replay: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PaneAttachResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cols = source["cols"];
	        this.rows = source["rows"];
	        this.seq = source["seq"];
	        this.preamble = source["preamble"];
	        this.replay = source["replay"];
	        this.running = source["running"];
	    }
	}
	export class Updater {
	
	
	    static createFrom(source: any = {}) {
	        return new Updater(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace core {
	
	export class AgentView {
	    kind: string;
	    installed: boolean;
	    enabled: boolean;
	    custom?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.installed = source["installed"];
	        this.enabled = source["enabled"];
	        this.custom = source["custom"];
	    }
	}
	export class FindCandidate {
	    label: string;
	    pane_id: number;
	
	    static createFrom(source: any = {}) {
	        return new FindCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.pane_id = source["pane_id"];
	    }
	}
	export class NodeView {
	    pane_id?: number;
	    vertical?: boolean;
	    ratio?: number;
	    a?: NodeView;
	    b?: NodeView;
	
	    static createFrom(source: any = {}) {
	        return new NodeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pane_id = source["pane_id"];
	        this.vertical = source["vertical"];
	        this.ratio = source["ratio"];
	        this.a = this.convertValues(source["a"], NodeView);
	        this.b = this.convertValues(source["b"], NodeView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PaneView {
	    pane_id: number;
	    name: string;
	    kind: string;
	    title?: string;
	    running: boolean;
	    starting?: boolean;
	    busy: boolean;
	    attention?: boolean;
	    unseen?: boolean;
	    failure?: string;
	    kitty_keys?: boolean;
	    anchor?: string;
	    width_pct?: number;
	    height_pct?: number;
	    agent?: string;
	
	    static createFrom(source: any = {}) {
	        return new PaneView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pane_id = source["pane_id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.running = source["running"];
	        this.starting = source["starting"];
	        this.busy = source["busy"];
	        this.attention = source["attention"];
	        this.unseen = source["unseen"];
	        this.failure = source["failure"];
	        this.kitty_keys = source["kitty_keys"];
	        this.anchor = source["anchor"];
	        this.width_pct = source["width_pct"];
	        this.height_pct = source["height_pct"];
	        this.agent = source["agent"];
	    }
	}
	export class SettingsPatch {
	    agent_enabled?: Record<string, boolean>;
	    default_agent?: string;
	    sound?: boolean;
	    sound_kind?: string;
	    notify?: boolean;
	    theme?: string;
	    auto_copy?: boolean;
	    split_direction?: string;
	    workspace_folding?: boolean;
	    sidebar_collapsed?: boolean;
	    subrepo_depth?: number;
	    prefs?: prefs.Prefs;
	
	    static createFrom(source: any = {}) {
	        return new SettingsPatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_enabled = source["agent_enabled"];
	        this.default_agent = source["default_agent"];
	        this.sound = source["sound"];
	        this.sound_kind = source["sound_kind"];
	        this.notify = source["notify"];
	        this.theme = source["theme"];
	        this.auto_copy = source["auto_copy"];
	        this.split_direction = source["split_direction"];
	        this.workspace_folding = source["workspace_folding"];
	        this.sidebar_collapsed = source["sidebar_collapsed"];
	        this.subrepo_depth = source["subrepo_depth"];
	        this.prefs = this.convertValues(source["prefs"], prefs.Prefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SettingsView {
	    agents: AgentView[];
	    default_agent: string;
	    sound: boolean;
	    sound_kind: string;
	    sound_kinds: string[];
	    notify: boolean;
	    notify_supported: boolean;
	    theme: string;
	    themes: string[];
	    auto_copy: boolean;
	    split_direction: string;
	    workspace_folding: boolean;
	    subrepo_depth: number;
	    prefs: prefs.Prefs;
	
	    static createFrom(source: any = {}) {
	        return new SettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agents = this.convertValues(source["agents"], AgentView);
	        this.default_agent = source["default_agent"];
	        this.sound = source["sound"];
	        this.sound_kind = source["sound_kind"];
	        this.sound_kinds = source["sound_kinds"];
	        this.notify = source["notify"];
	        this.notify_supported = source["notify_supported"];
	        this.theme = source["theme"];
	        this.themes = source["themes"];
	        this.auto_copy = source["auto_copy"];
	        this.split_direction = source["split_direction"];
	        this.workspace_folding = source["workspace_folding"];
	        this.subrepo_depth = source["subrepo_depth"];
	        this.prefs = this.convertValues(source["prefs"], prefs.Prefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SubrepoView {
	    path: string;
	    rel: string;
	    branch?: string;
	    ahead?: number;
	    behind?: number;
	    changed?: number;
	    conflicts?: number;
	
	    static createFrom(source: any = {}) {
	        return new SubrepoView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.rel = source["rel"];
	        this.branch = source["branch"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.changed = source["changed"];
	        this.conflicts = source["conflicts"];
	    }
	}
	export class TabView {
	    name?: string;
	    focused_pane_id: number;
	    layout?: NodeView;
	    panes: PaneView[];
	    floats?: PaneView[];
	
	    static createFrom(source: any = {}) {
	        return new TabView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.focused_pane_id = source["focused_pane_id"];
	        this.layout = this.convertValues(source["layout"], NodeView);
	        this.panes = this.convertValues(source["panes"], PaneView);
	        this.floats = this.convertValues(source["floats"], PaneView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ThemeView {
	    name: string;
	    colors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ThemeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.colors = source["colors"];
	    }
	}
	export class WorkspaceView {
	    group_path?: string[];
	    name: string;
	    path: string;
	    branch?: string;
	    folded?: boolean;
	    active: number;
	    tabs: TabView[];
	    subrepos?: SubrepoView[];
	    hidden_subrepos?: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group_path = source["group_path"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.branch = source["branch"];
	        this.folded = source["folded"];
	        this.active = source["active"];
	        this.tabs = this.convertValues(source["tabs"], TabView);
	        this.subrepos = this.convertValues(source["subrepos"], SubrepoView);
	        this.hidden_subrepos = source["hidden_subrepos"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class View {
	    version: string;
	    workspaces: WorkspaceView[];
	    selected: number;
	    sidebar_collapsed: boolean;
	    sidebar_width: number;
	    split_direction: string;
	    workspace_folding: boolean;
	    default_agent: string;
	    agents: AgentView[];
	    theme: ThemeView;
	    keymap: Record<string, string>;
	    prefix: string;
	    nav_keys: Record<string, string>;
	    persisting: boolean;
	    auto_copy: boolean;
	    menus: api.MenuRegister[];
	    prefs: prefs.Prefs;
	    problems?: string[];
	    blocked?: string;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceView);
	        this.selected = source["selected"];
	        this.sidebar_collapsed = source["sidebar_collapsed"];
	        this.sidebar_width = source["sidebar_width"];
	        this.split_direction = source["split_direction"];
	        this.workspace_folding = source["workspace_folding"];
	        this.default_agent = source["default_agent"];
	        this.agents = this.convertValues(source["agents"], AgentView);
	        this.theme = this.convertValues(source["theme"], ThemeView);
	        this.keymap = source["keymap"];
	        this.prefix = source["prefix"];
	        this.nav_keys = source["nav_keys"];
	        this.persisting = source["persisting"];
	        this.auto_copy = source["auto_copy"];
	        this.menus = this.convertValues(source["menus"], api.MenuRegister);
	        this.prefs = this.convertValues(source["prefs"], prefs.Prefs);
	        this.problems = source["problems"];
	        this.blocked = source["blocked"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace prefs {
	
	export class Window {
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	    maximized: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Window(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.maximized = source["maximized"];
	    }
	}
	export class Prefs {
	    window?: Window;
	    font_family: string;
	    font_size?: number;
	    line_height?: number;
	    cursor_blink?: boolean;
	    scrollback_lines?: number;
	    sidebar_width?: number;
	
	    static createFrom(source: any = {}) {
	        return new Prefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.window = this.convertValues(source["window"], Window);
	        this.font_family = source["font_family"];
	        this.font_size = source["font_size"];
	        this.line_height = source["line_height"];
	        this.cursor_blink = source["cursor_blink"];
	        this.scrollback_lines = source["scrollback_lines"];
	        this.sidebar_width = source["sidebar_width"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace updater {
	
	export class Info {
	    current: string;
	    latest: string;
	    available: boolean;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.url = source["url"];
	    }
	}

}

