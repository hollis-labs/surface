/**
 * Operations: the board, a workstream's drill-in, and the seed preview. The server holds the
 * data (Caster, ops/v1) and the windows; these views draw what the routes and the event
 * stream (ops.changed, ops.activity) say.
 */
import type { ModuleWeb } from "@hollis-labs/surface-page/sdk"
import { Board } from "./board"
import { SeedView } from "./seed"
import { WorkstreamView } from "./workstream"

export default {
  views: { "ops/board": Board, "ops/workstream": WorkstreamView, "ops/seed": SeedView },
} satisfies ModuleWeb
