import type { SubrepoView } from "./types"

/** One sidebar row: a repository, or a folder that only leads to deeper ones. */
export interface SubrepoNode {
  level: number
  name: string
  /** Index into the repository list, -1 for a plain folder. */
  repo: number
  /** Last child at its level, drawn with └ instead of ├. */
  last: boolean
}

/**
 * Builds the TUI's tree rows from the core's sorted, depth-first scan:
 * each shared parent folder appears once, above the repositories it holds.
 */
export function subrepoTree(repos: SubrepoView[]): SubrepoNode[] {
  const nodes: SubrepoNode[] = []
  let previous: string[] = []
  repos.forEach((repo, index) => {
    const parts = repo.rel.split("/")
    const parents = parts.slice(0, -1)
    let shared = 0
    while (shared < parents.length && shared < previous.length && parents[shared] === previous[shared]) shared++
    for (let level = shared; level < parents.length; level++) {
      nodes.push({ level, name: parents[level]!, repo: -1, last: false })
    }
    nodes.push({ level: parents.length, name: parts[parts.length - 1]!, repo: index, last: false })
    previous = parents
  })
  nodes.forEach((node, index) => {
    node.last = true
    for (const next of nodes.slice(index + 1)) {
      if (next.level < node.level) break
      if (next.level === node.level) {
        node.last = false
        break
      }
    }
  })
  return nodes
}
