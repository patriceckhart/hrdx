/**
 * Final index for moving item `from` so it lands before (or after) item
 * `over`, given the core's remove-then-insert semantics. Returns null when
 * the move would not change the order.
 */
export function reorderTarget(from: number, over: number, after: boolean): number | null {
  if (from < 0 || over < 0) return null
  let to = after ? over + 1 : over
  if (to > from) to -= 1
  return to === from ? null : to
}

/** Pixels the pointer must travel before a press becomes a drag. */
export const DRAG_THRESHOLD = 4
