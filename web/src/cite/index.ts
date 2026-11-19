/**
 * Cite: references in any text (a Torque task id, a Tesseract item, a path, a fragment, a URL)
 * as chips with a preview card and an open action. Detection is ./core (framework-free);
 * resolution is the app's server, which asks the module that owns each kind.
 */
export { CiteRecordView } from "./record.tsx"
export { CITE_ATTR, CITE_ELEMENT, createRemarkCite } from "./remark.ts"
export { canResolve, initCite, kindLabel, openCite, registry } from "./store.ts"
export { CiteRef, CiteText, citeMarkdownExtensions, remarkCite } from "./ui.tsx"
