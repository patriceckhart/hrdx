import { describe, expect, test } from "bun:test"
import { splitPath } from "../components/ReviewPanel"
import { togglePanel, uiState } from "../lib/ui"

describe("review panel", () => {
  test("splitPath separates folder and file name", () => {
    expect(splitPath("internal/review/files.go")).toEqual(["internal/review/", "files.go"])
    expect(splitPath("README.md")).toEqual(["", "README.md"])
  })

  test("the same button closes its panel, the other one switches", () => {
    // Closing refocuses the terminal on the next frame, which Bun lacks.
    const scope = globalThis as { requestAnimationFrame?: (callback: () => void) => number }
    scope.requestAnimationFrame ??= () => 0
    togglePanel("files")
    expect(uiState().panel).toBe("files")
    togglePanel("changes")
    expect(uiState().panel).toBe("changes")
    togglePanel("changes")
    expect(uiState().panel).toBeNull()
  })
})
