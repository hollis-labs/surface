/**
 * @hollis-labs/surface-page/sdk: everything a module's web code may import from Surface.
 */

export { describeToolCall } from "../agent/ActivityTimeline"
export { baseToolName, ItemView as ChatItemView } from "../agent/items"
export { claimCards, type ServerCard } from "../cards/store"
export { RecordBody } from "../cite/record.tsx"
export { citeTerminalLinks } from "../cite/terminal.tsx"
export { CiteRef, CiteText, citeMarkdownExtensions, remarkCite } from "../cite/ui.tsx"
export {
  type NavGroupDef,
  type NavItemDef,
  type NavTreeNode,
  registerNavGroup,
  registerNavItem,
  useNavTree,
} from "../desk/navGroups"
export { ObjectActionsMenu, type ObjectActionsMenuProps } from "../desk/ObjectActionsMenu"
export { useAnchor } from "./anchors"
export { useEvent, useMarks, useSetting } from "./hooks"
export { Icon } from "./icon"
export { addMark, replyMark, resolveMark } from "./marks"
export {
  formatObjectRef,
  getObjectActions,
  isObjectBookmarked,
  isObjectPinned,
  listBookmarkedObjects,
  listPinnedObjects,
  type ObjectActionContext,
  type ObjectActionDef,
  type ObjectActionGroup,
  type ObjectRef,
  registerObjectAction,
  toggleObjectBookmark,
  toggleObjectPin,
  useObjectActions,
} from "./objectActions"
export { ApiError, setSetting } from "./runtime"
export * from "./types"
export { isOwnerTyping } from "./typing"
