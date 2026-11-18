export type { CardEngine } from "./engine.ts"
export { createCardEngine, memoryStore } from "./engine.ts"
export type { ExpiryDisplay } from "./expiry.ts"
export { describeExpiry } from "./expiry.ts"
export { dedupeKeyFor, matches, pickRule } from "./match.ts"
export type {
  CardInput,
  CardRule,
  CardStatus,
  CardViewModel,
  EngineOptions,
  EngineSnapshot,
  EngineStore,
  HeldEntry,
  MatchValue,
  RuleAction,
  RuleMatch,
  Surface,
} from "./types.ts"
