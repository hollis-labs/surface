import { TrackersWorkbench } from "../../../web/src/desk/TrackersWorkbench"
import type { ModuleWeb } from "../../../web/src/sdk"

export const web: ModuleWeb = {
  views: {
    "trackers/main": TrackersWorkbench,
  },
}
export default web
