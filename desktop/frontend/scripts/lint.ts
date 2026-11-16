// Fails when server-side Next.js features appear. The frontend is a static
// export embedded in the binary: no proxy, middleware, route handlers,
// server actions, or module-scope access to the Wails bindings.

import { readdirSync, readFileSync, statSync } from "node:fs"
import { join, relative } from "node:path"

const root = join(import.meta.dir, "..")
const skip = new Set(["node_modules", ".next", "out", "wailsjs", "tests", "scripts"])
const problems: string[] = []

function walk(dir: string): string[] {
  const files: string[] = []
  for (const name of readdirSync(dir)) {
    if (skip.has(name)) continue
    const path = join(dir, name)
    if (statSync(path).isDirectory()) files.push(...walk(path))
    else if (/\.(ts|tsx)$/.test(name)) files.push(path)
  }
  return files
}

for (const forbidden of ["proxy.ts", "middleware.ts", "app/api"]) {
  try {
    statSync(join(root, forbidden))
    problems.push(`${forbidden}: server features are not allowed in the static export`)
  } catch {
    // absent, as required
  }
}

for (const file of walk(root)) {
  const rel = relative(root, file)
  const source = readFileSync(file, "utf8")
  if (/\/route\.(ts|tsx)$/.test(rel)) problems.push(`${rel}: route handlers are not allowed`)
  if (/^\s*["']use server["']/m.test(source)) problems.push(`${rel}: server actions are not allowed`)
  if (/from\s+["']next\/(headers|server)["']/.test(source)) problems.push(`${rel}: server-only imports are not allowed`)
  // Bindings must be read inside calls: no window.go or window.runtime at module scope.
  for (const [index, line] of source.split("\n").entries()) {
    if (/^(const|let|var|export const)\s.*\bwindow\.(go|runtime)\b/.test(line)) {
      problems.push(`${rel}:${index + 1}: Wails bindings accessed at module scope`)
    }
  }
  if (rel.startsWith("components/") && !rel.endsWith(".css") && !/^["']use client["']/.test(source.trimStart())) {
    problems.push(`${rel}: components must be client components`)
  }
}

if (problems.length > 0) {
  for (const problem of problems) console.error(problem)
  process.exit(1)
}
console.log("lint: ok")
