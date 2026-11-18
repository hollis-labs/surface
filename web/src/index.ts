/**
 * @hollis-labs/surface-page: the core of a Surface app's page. An app's entry is
 *
 *   import { mountPage } from "@hollis-labs/surface-page"
 *   mountPage({ modules })
 *
 * with `modules` its modules' views. A module's own web code imports only
 * "@hollis-labs/surface-page/sdk". The drawing is the app's installed registry components
 * (`@/components/ui/*`); see registry/README.md, "Using it in an app".
 */
export { mountPage, Page, type PageProps } from "./boot"
export { type CardRenderer, type CardRendererProps, registerCard } from "./cards/renderers"
export { CardHost, CardsWindow } from "./cards/views"
export { coreWeb } from "./desk/coreViews"
export { Desktop, type DesktopProps } from "./desk/Desktop"
