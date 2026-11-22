import { AlignWorkbench } from "../../../web/src/desk/AlignWorkbench"
import type { ModuleWeb } from "../../../web/src/sdk"

export const web: ModuleWeb = {
  views: {
    "align/main": AlignWorkbench,
  },
}
export default web
