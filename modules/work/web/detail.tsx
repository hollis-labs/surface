/**
 * An item's detail, beside the view: title, stage and fields, the body's sections edited in
 * place, tags, the Torque link, links both ways, material and the publication log, the review
 * (notes, rounds, sign-off), and the item's revisions with a word diff against any of them.
 */
import { CiteText, type Desk, Icon, ObjectActionsMenu } from "@hollis-labs/surface-page/sdk"
import { type ReactNode, useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import type {
  WorkCard,
  WorkClient,
  WorkDetail,
  WorkEdit,
  WorkField,
  WorkKind,
  WorkLink,
  WorkMaterial,
  WorkPublished,
  WorkRevision,
} from "./api"
import { KindIcon, Pick, Sec } from "./parts"
import { Review } from "./review"
import { ago, diffWords, label, message, ymd } from "./util"

/** A reference shown as a link when it is one, else as text. */
/** A material reference as its Cite chip (a Caster doc, a fragment, a Tesseract item, a link), as Basalisk shows it. */
function Ref({ text }: { text: string }) {
  return (
    <span className="min-w-0 truncate">
      <CiteText text={text} />
    </span>
  )
}

export function Detail({
  w,
  desk,
  ws,
  id,
  kinds,
  items,
  revOnBoard,
  close,
  editing,
  changed,
  open,
  popOut,
}: {
  w: WorkClient
  desk: Desk
  ws: string
  id: string
  kinds: Map<string, WorkKind>
  items: WorkCard[]
  /** Open another item in the detail. */
  open: (id: string, newWindow?: boolean) => void
  /** Pop this item out into its own Work window. */
  popOut?: () => void
  /** The item's revision in the view's copy: a newer one reloads the detail. */
  revOnBoard: number
  close: () => void
  editing: (on: boolean) => void
  changed: () => void
}) {
  const [d, setD] = useState<WorkDetail | null>(null)
  const [error, setError] = useState("")
  const [status, setStatus] = useState("")
  const [edit, setEdit] = useState<{ section: string; text: string } | null>(null)
  const [title, setTitle] = useState("")
  const [showRevs, setShowRevs] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await w.item(ws, id)
      setD(r)
      setTitle(r.title)
      setError("")
    } catch (e) {
      setError(message(e))
    }
  }, [w, ws, id])
  // A newer revision in the view's copy (work.changed read back) reloads it, unless a section is open.
  // biome-ignore lint/correctness/useExhaustiveDependencies: a newer revision on the board reloads it
  useEffect(() => {
    if (!edit) void load()
  }, [load, revOnBoard])

  const head = (body: ReactNode) => (
    <header className="flex items-center gap-1.5 px-3 py-2">
      {body}
      <span className="ml-auto flex items-center gap-0.5">
        {d && (
          <ObjectActionsMenu
            object={{
              kind: "work.item",
              id: d.id,
              title: d.title,
              subtitle: d.stage,
            }}
          />
        )}
        {popOut && (
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={popOut}
            title="Open item in new window"
            aria-label="Open item in new window"
          >
            <Icon name="external-link" />
          </Button>
        )}
        <Button variant="ghost" size="icon-xs" onClick={close} title="Close" aria-label="Close">
          <Icon name="x" />
        </Button>
      </span>
    </header>
  )
  if (!d)
    return (
      <aside className="flex w-96 shrink-0 flex-col border-l">
        {head(<span className="text-muted-foreground text-sm">{error || "…"}</span>)}
      </aside>
    )

  const k = kinds.get(d.kind)
  const save = async (change: Omit<WorkEdit, "base_rev">) => {
    setStatus("Saving…")
    try {
      const r = await w.edit(ws, id, { base_rev: d.rev, ...change })
      setStatus(`Saved as revision ${r.rev}.`)
    } catch (e) {
      // 409: changed in Caster meanwhile; the owner's words became suggestions.
      setStatus(message(e))
    }
    editing(false)
    await load()
    changed()
  }
  const titleOf = (ref: string) => items.find((x) => x.id === ref)?.title ?? ref
  const channelOf = (v: unknown) => (typeof v === "string" && v ? titleOf(v) : "")
  const startEdit = (sec: string) => {
    editing(true)
    setEdit({ section: sec, text: d.sections[sec] ?? "" })
  }

  return (
    <aside className="flex w-96 shrink-0 flex-col border-l">
      {head(
        <>
          <KindIcon kind={k} size={14} />
          <Input
            className="h-7 flex-1"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onBlur={() => title.trim() && title !== d.title && void save({ title })}
            onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
            aria-label="Title"
          />
        </>,
      )}
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex items-center gap-1.5 px-3 pb-2 text-muted-foreground text-xs">
          <span title={d.ref}>
            {k?.label ?? d.kind} · {d.id} · revision {d.rev}
          </span>
          <span className="ml-auto" />
          {k && k.stages.length > 0 && (
            <Pick
              label="Stage"
              value={d.stage}
              options={k.stages.map((s) => ({ value: s, label: label(s) }))}
              onChange={async (v) => {
                setStatus("")
                try {
                  await w.move(ws, id, "stage", v)
                } catch (err) {
                  setStatus(message(err))
                }
                await load()
                changed()
              }}
            />
          )}
          <Button
            variant="ghost"
            size={d.cut ? "sm" : "icon-sm"}
            title={d.cut ? "Put it back" : "Cut: take it out of the views (it stays in Caster)"}
            aria-label={d.cut ? "Restore" : "Cut"}
            onClick={async () => {
              try {
                if (d.cut) await w.restore(ws, id)
                else await w.cut(ws, id)
              } catch (e) {
                setStatus(message(e))
              }
              await load()
              changed()
            }}
          >
            {d.cut ? "Restore" : <Icon name="scissors" />}
          </Button>
        </div>
        {d.problem && <p className="px-3 pb-2 text-warning text-xs">{d.problem}</p>}
        {status && <p className="px-3 pb-2 text-muted-foreground text-xs">{status}</p>}

        {k && k.fields.length > 0 && (
          <div className="grid grid-cols-[7rem_1fr] items-center gap-x-2 gap-y-1.5 border-t px-3 py-2.5">
            {k.fields.map((f) => (
              <FieldRow
                key={f.key}
                field={f}
                self={d.id}
                value={d.fields[f.key]}
                items={items}
                set={(v) => void save({ fields: { [f.key]: v } })}
              />
            ))}
          </div>
        )}

        {d.lead && (
          <div className="border-t px-3 py-2.5">
            <Markdown>{d.lead}</Markdown>
          </div>
        )}
        {d.order.map((sec) => (
          <Sec key={sec} title={sec}>
            {edit?.section === sec ? (
              <Textarea
                autoFocus
                value={edit.text}
                onChange={(e) => setEdit({ section: sec, text: e.target.value })}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    setEdit(null)
                    editing(false)
                  } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) e.currentTarget.blur()
                }}
                onBlur={() => {
                  const text = edit.text
                  setEdit(null)
                  if (text.trim() !== (d.sections[sec] ?? "").trim()) void save({ sections: { [sec]: text } })
                  else editing(false)
                }}
                rows={Math.max(4, edit.text.split("\n").length + 1)}
              />
            ) : (
              <div
                className={`cursor-text rounded-sm text-sm wrap-anywhere hover:bg-accent ${d.sections[sec] ? "" : "text-muted-foreground"}`}
                data-section={sec}
                onClick={(e) => {
                  // Selecting a passage (to suggest a change to it) is not a click to edit, nor is following a link.
                  if (window.getSelection()?.toString() || (e.target as Element).closest("a")) return
                  startEdit(sec)
                }}
                title="Click to edit (⌘↩ or clicking away keeps it, Esc does not)"
              >
                {d.sections[sec] ? <Markdown>{d.sections[sec]}</Markdown> : "Click to write"}
              </div>
            )}
          </Sec>
        ))}

        <Sec title="Tags">
          <TagsRow tags={d.tags} set={(tags) => void save({ tags })} />
        </Sec>

        {(d.task || ["task", "feature", "exploration"].includes(d.kind)) && (
          <Sec title="Torque">
            <TorqueRow
              w={w}
              desk={desk}
              ws={ws}
              id={id}
              task={d.task}
              kind={k?.label ?? d.kind}
              title={d.title}
              sent={() => {
                void load()
                changed()
              }}
            />
          </Sec>
        )}
        <Sec title="Links">
          <Links d={d} items={items} kinds={kinds} open={open} set={(links) => void save({ links })} />
        </Sec>
        <Sec title="Material">
          <MaterialList material={d.material} set={(material) => void save({ material })} />
        </Sec>
        {(k?.stages.includes("published") || d.published.length > 0) && (
          <Sec title="Published">
            <PublicationLog
              log={d.published}
              where={channelOf(d.fields.channel)}
              set={(published) => void save({ published })}
            />
          </Sec>
        )}

        <Review w={w} desk={desk} ws={ws} d={d} reload={load} changed={changed} />

        <Sec
          title="Revisions"
          actions={
            <Button variant="link" size="xs" onClick={() => setShowRevs((s) => !s)}>
              {showRevs ? "Hide" : "Show"}
            </Button>
          }
        >
          {showRevs && <Revisions w={w} ws={ws} d={d} />}
        </Sec>
      </ScrollArea>
    </aside>
  )
}

/** One field of the item, edited by its type. */
function FieldRow({
  field,
  self,
  value,
  items,
  set,
}: {
  field: WorkField
  self: string
  value: unknown
  items: WorkCard[]
  set: (v: unknown) => void
}) {
  // A ref field's options name the kinds it may point at (empty: any kind).
  const targets = items.filter(
    (x) =>
      !x.cut && x.id !== self && (field.type === "enum" || !field.options?.length || field.options.includes(x.kind)),
  )
  const [text, setText] = useState(value == null ? "" : String(value))
  useEffect(() => setText(value == null ? "" : String(value)), [value])
  const name = field.label || field.key
  const commit = (v: string) => {
    if (v === (value == null ? "" : String(value))) return
    if (field.type === "number") set(v.trim() === "" ? null : Number(v))
    else set(v.trim() === "" ? null : v)
  }
  let input: ReactNode
  switch (field.type) {
    case "enum":
      input = (
        <Pick
          label={name}
          value={text}
          onChange={commit}
          options={[{ value: "", label: "—" }, ...(field.options ?? []).map((o) => ({ value: o, label: o }))]}
        />
      )
      break
    case "bool":
      input = <Checkbox checked={value === true} onCheckedChange={(on) => set(on)} aria-label={name} />
      break
    case "ref":
      input = (
        <Pick
          label={name}
          value={text}
          onChange={commit}
          options={[
            { value: "", label: "—" },
            ...(text && !targets.some((x) => x.id === text)
              ? [{ value: text, label: items.find((x) => x.id === text)?.title ?? text }]
              : []),
            ...targets.map((x) => ({ value: x.id, label: x.title })),
          ]}
        />
      )
      break
    case "refs": {
      const list = Array.isArray(value) ? value.map(String) : []
      input = (
        <span className="flex flex-wrap items-center gap-1">
          {list.map((r) => (
            <Badge key={r} variant="secondary">
              {items.find((x) => x.id === r)?.title ?? r}
              <Button
                variant="ghost"
                size="icon-xs"
                title="Take it out"
                aria-label="Take it out"
                onClick={() => {
                  const next = list.filter((x) => x !== r)
                  set(next.length ? next : null)
                }}
              >
                <Icon name="x" />
              </Button>
            </Badge>
          ))}
          <Pick
            label={`Add to ${name}`}
            value=""
            onChange={(v) => v && set([...list, v])}
            options={[
              { value: "", label: "+ add" },
              ...targets.filter((x) => !list.includes(x.id)).map((x) => ({ value: x.id, label: x.title })),
            ]}
          />
        </span>
      )
      break
    }
    default:
      input = (
        <Input
          className="h-7"
          type={field.type === "date" ? "date" : field.type === "number" ? "number" : "text"}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={() => commit(text)}
          onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
          placeholder={field.type === "url" ? "https://…" : ""}
          aria-label={name}
        />
      )
  }
  return (
    <>
      <span className="truncate text-muted-foreground text-xs">{name}</span>
      <div className="min-w-0">{input}</div>
    </>
  )
}

function TagsRow({ tags, set }: { tags: string[]; set: (t: string[]) => void }) {
  const [text, setText] = useState(tags.join(", "))
  useEffect(() => setText(tags.join(", ")), [tags])
  return (
    <Input
      className="h-7"
      value={text}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => {
        const next = text
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean)
        if (next.join(",") !== tags.join(",")) set(next)
      }}
      onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
      placeholder="comma, separated"
      aria-label="Tags"
    />
  )
}

/** The item's revisions; picking one shows what changed from it to the current revision. */
function Revisions({ w, ws, d }: { w: WorkClient; ws: string; d: WorkDetail }) {
  const name = `i.${d.id}`
  const [revs, setRevs] = useState<WorkRevision[] | null>(null)
  const [from, setFrom] = useState<number | null>(null)
  const [before, setBefore] = useState<{ title?: string; stage?: string; sections: Record<string, string> } | null>(
    null,
  )
  const [error, setError] = useState("")
  useEffect(() => {
    w.log(ws, name)
      .then((r) => setRevs(r.revisions))
      .catch((e) => setError(message(e)))
  }, [w, ws, name])
  useEffect(() => {
    if (from == null) return
    setBefore(null)
    w.at(ws, name, from)
      .then((r) => setBefore({ title: r.title, stage: r.stage, sections: r.sections ?? {} }))
      .catch((e) => setError(message(e)))
  }, [w, ws, name, from])
  if (error) return <p className="text-warning text-xs">{error}</p>
  if (!revs) return <p className="text-muted-foreground text-xs">…</p>
  const norm = (h: string) => h.toLowerCase().replace(/[\s_-]/g, "")
  const sectionAt = (secs: Record<string, string>, h: string) =>
    Object.entries(secs).find(([k]) => norm(k) === norm(h))?.[1] ?? ""
  const changedSecs = before
    ? d.order.filter((s) => sectionAt(before.sections, s).trim() !== (d.sections[s] ?? "").trim())
    : []
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-1">
        {revs.map((r) => (
          <Button
            key={r.rev}
            variant={from === r.rev ? "secondary" : "outline"}
            size="xs"
            disabled={r.rev === d.rev}
            onClick={() => setFrom(r.rev)}
            title={r.rev === d.rev ? "The current revision" : "Compare with the current revision"}
          >
            {r.rev}
            {r.at ? ` · ${ago(r.at)}` : ""}
          </Button>
        ))}
      </div>
      {before && from != null && (
        <div className="flex flex-col gap-2 text-sm">
          <div className="text-muted-foreground text-xs">
            Revision {from} → {d.rev}
            {before.title !== d.title ? ` · title was "${before.title}"` : ""}
            {before.stage !== d.stage ? ` · stage was ${before.stage || "—"}` : ""}
          </div>
          {changedSecs.length === 0 && (
            <div className="text-muted-foreground text-xs">No section changed; only its details did.</div>
          )}
          {changedSecs.map((sec) => (
            <div key={sec} className="work-diff">
              <b className="text-xs">{sec}</b>
              <div className="work-text">
                {diffWords(sectionAt(before.sections, sec), d.sections[sec] ?? "").map((p, i) =>
                  p.op === "same" ? (
                    // biome-ignore lint/suspicious/noArrayIndexKey: parts of one fixed diff
                    <span key={i}>{p.text}</span>
                  ) : p.op === "del" ? (
                    // biome-ignore lint/suspicious/noArrayIndexKey: parts of one fixed diff
                    <del key={i}>{p.text}</del>
                  ) : (
                    // biome-ignore lint/suspicious/noArrayIndexKey: parts of one fixed diff
                    <ins key={i}>{p.text}</ins>
                  ),
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** Material: references to things kept elsewhere, never copies. Add one by pasting its reference. */
function MaterialList({ material, set }: { material: WorkMaterial[]; set: (m: WorkMaterial[]) => void }) {
  const [ref, setRef] = useState("")
  const add = () => {
    const r = ref.trim()
    setRef("")
    if (!r || material.some((x) => x.ref === r)) return
    set([...material, { ref: r }])
  }
  return (
    <div className="flex flex-col gap-1.5">
      {material.length === 0 && <div className="text-muted-foreground text-xs">Nothing attached.</div>}
      {material.map((m, i) => (
        <div key={m.ref} className="flex min-w-0 items-center gap-1.5 text-sm">
          <Ref text={m.ref} />
          <Caption
            value={m.caption ?? ""}
            set={(caption) => set(material.map((x, j) => (j === i ? { ...x, caption: caption || undefined } : x)))}
          />
          <Button
            variant="ghost"
            size="icon-xs"
            title="Take it off (the thing itself is not touched)"
            aria-label="Take it off"
            onClick={() => set(material.filter((_, j) => j !== i))}
          >
            <Icon name="x" />
          </Button>
        </div>
      ))}
      <div className="flex items-center gap-1.5">
        <Input
          className="h-7 flex-1"
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && add()}
          placeholder="Paste a reference: tach:fragment/…, caster://…, a link"
          aria-label="Reference"
        />
        <Button variant="outline" size="sm" disabled={!ref.trim()} onClick={add}>
          Add
        </Button>
      </div>
    </div>
  )
}

function Caption({ value, set }: { value: string; set: (v: string) => void }) {
  const [text, setText] = useState(value)
  useEffect(() => setText(value), [value])
  return (
    <Input
      className="h-6 w-28 shrink-0"
      value={text}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => text.trim() !== value && set(text.trim())}
      onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
      placeholder="caption"
      aria-label="Caption"
    />
  )
}

/**
 * The publication log: when and where it went out, with the address. Publishing is not done
 * here; this records it.
 */
function PublicationLog({
  log,
  where,
  set,
}: {
  log: WorkPublished[]
  where: string
  set: (l: WorkPublished[]) => void
}) {
  const [editing, setEditing] = useState<number | null>(null)
  const blank = (): WorkPublished => ({ at: ymd(new Date()), where, url: "", note: "" })
  return (
    <div className="flex flex-col gap-1.5">
      {log.length === 0 && editing === null && <div className="text-muted-foreground text-xs">Not published yet.</div>}
      {log.map((p, i) =>
        editing === i ? (
          <PublicationForm
            // biome-ignore lint/suspicious/noArrayIndexKey: entries may repeat; the log's order is its identity
            key={`${p.at}:${p.where}:${i}`}
            start={p}
            done={(next) => {
              setEditing(null)
              if (next) set(log.map((x, j) => (j === i ? next : x)))
            }}
          />
        ) : (
          // biome-ignore lint/suspicious/noArrayIndexKey: entries may repeat; the log's order is its identity
          <div key={`${p.at}:${p.where}:${i}`} className="flex min-w-0 items-center gap-1.5 text-sm">
            <Button variant="link" size="xs" onClick={() => setEditing(i)} title="Change this entry">
              {p.at.slice(0, 10)} · {p.where}
            </Button>
            {p.url && <Ref text={p.url} />}
            {p.note && <span className="truncate text-muted-foreground text-xs">{p.note}</span>}
            <Button
              variant="ghost"
              size="icon-xs"
              className="ml-auto"
              title="Take this entry out of the log"
              aria-label="Take this entry out"
              onClick={() => set(log.filter((_, j) => j !== i))}
            >
              <Icon name="x" />
            </Button>
          </div>
        ),
      )}
      {editing === -1 ? (
        <PublicationForm
          start={blank()}
          done={(next) => {
            setEditing(null)
            if (next) set([...log, next])
          }}
        />
      ) : (
        editing === null && (
          <Button variant="link" size="xs" className="self-start" onClick={() => setEditing(-1)}>
            + Log a publication
          </Button>
        )
      )}
    </div>
  )
}

function PublicationForm({ start, done }: { start: WorkPublished; done: (p?: WorkPublished) => void }) {
  const [p, setP] = useState(start)
  const ok = /^\d{4}-\d{2}-\d{2}/.test(p.at) && p.where.trim() !== ""
  const keep = () =>
    ok && done({ at: p.at, where: p.where.trim(), url: p.url?.trim() || undefined, note: p.note?.trim() || undefined })
  return (
    <div className="grid grid-cols-2 gap-1.5" onKeyDown={(e) => e.key === "Escape" && done()}>
      <Input
        className="h-7"
        type={/^\d{4}-\d{2}-\d{2}$/.test(p.at) ? "date" : "text"}
        value={p.at}
        onChange={(e) => setP({ ...p, at: e.target.value })}
        aria-label="When"
      />
      <Input
        className="h-7"
        value={p.where}
        onChange={(e) => setP({ ...p, where: e.target.value })}
        placeholder="Where (YouTube, the blog…)"
        aria-label="Where"
      />
      <Input
        className="h-7"
        value={p.url ?? ""}
        onChange={(e) => setP({ ...p, url: e.target.value })}
        placeholder="https://…"
        aria-label="Address"
      />
      <Input
        className="h-7"
        value={p.note ?? ""}
        onChange={(e) => setP({ ...p, note: e.target.value })}
        placeholder="Note (optional)"
        aria-label="Note"
      />
      <div className="col-span-2 flex gap-1.5">
        <Button size="sm" disabled={!ok} onClick={keep}>
          Keep
        </Button>
        <Button variant="ghost" size="sm" onClick={() => done()}>
          Cancel
        </Button>
      </div>
    </div>
  )
}

/** Relations an item can carry (contains is the outline: parent and children). */
const RELS = ["appears-in", "leads-to", "depends-on", "uses", "relates"]

/**
 * Links both ways: where the item sits (its parent, what it holds), its own typed links
 * (edited here), and what points at it: other items' links and their ref fields.
 */
function Links({
  d,
  items,
  kinds,
  open,
  set,
}: {
  d: WorkDetail
  items: WorkCard[]
  kinds: Map<string, WorkKind>
  open: (id: string, newWindow?: boolean) => void
  set: (links: WorkLink[]) => void
}) {
  const [rel, setRel] = useState(RELS[0])
  const byId = new Map(items.map((x) => [x.id, x]))
  const go = (id: string) =>
    id.includes("://") ? (
      <Ref text={id} />
    ) : (
      <Button
        variant="link"
        size="xs"
        className="h-auto min-w-0 max-w-full px-0"
        onClick={(e) => open(id, e.metaKey || e.ctrlKey || e.shiftKey)}
        title={`${id} (⌘-click to open in new window)`}
      >
        <span className="truncate">{byId.get(id)?.title ?? id}</span>
      </Button>
    )
  const incoming: { from: string; how: string }[] = []
  for (const x of items) {
    if (x.cut || x.id === d.id) continue
    for (const l of x.links) if (l.to === d.id) incoming.push({ from: x.id, how: l.rel })
    for (const f of kinds.get(x.kind)?.fields ?? []) {
      const v = x.fields[f.key]
      if ((f.type === "ref" && v === d.id) || (f.type === "refs" && Array.isArray(v) && v.includes(d.id)))
        incoming.push({ from: x.id, how: (f.label || f.key).toLowerCase() })
    }
  }
  const candidates = items.filter(
    (x) => !x.cut && x.id !== d.id && !d.links.some((l) => l.rel === rel && l.to === x.id),
  )
  return (
    <div className="flex flex-col gap-1 text-sm">
      {d.parent && (
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="text-muted-foreground text-xs">in</span> {go(d.parent)}
        </div>
      )}
      {d.children.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-1.5">
          <span className="text-muted-foreground text-xs">holds</span>
          {d.children.map((c) => (
            <span key={c}>{go(c)}</span>
          ))}
        </div>
      )}
      {d.links.map((l, i) => (
        <div key={`${l.rel}:${l.to}`} className="flex min-w-0 items-center gap-1.5">
          <span className="text-muted-foreground text-xs">{l.rel}</span> {go(l.to)}
          <Button
            variant="ghost"
            size="icon-xs"
            className="ml-auto"
            title="Take this link out"
            aria-label="Take this link out"
            onClick={() => set(d.links.filter((_, j) => j !== i))}
          >
            <Icon name="x" />
          </Button>
        </div>
      ))}
      <div className="flex items-center gap-1.5">
        <Pick label="Relation" value={rel} onChange={setRel} options={RELS.map((r) => ({ value: r, label: r }))} />
        <Pick
          label="Link to"
          className="min-w-0 flex-1"
          value=""
          onChange={(v) => v && set([...d.links, { rel, to: v }])}
          options={[
            { value: "", label: "+ link to…" },
            ...candidates.map((x) => ({ value: x.id, label: `${kinds.get(x.kind)?.label ?? x.kind} · ${x.title}` })),
          ]}
        />
      </div>
      {incoming.length > 0 && (
        <>
          <div className="pt-1 text-muted-foreground text-xs">Pointing here</div>
          {incoming.map((x, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: one item may point here twice
            <div key={`${x.from}:${x.how}:${i}`} className="flex min-w-0 items-center gap-1.5">
              {go(x.from)} <span className="text-muted-foreground text-xs">{x.how}</span>
            </div>
          ))}
        </>
      )}
    </div>
  )
}

/**
 * A planner task's Torque task: send it (once; a second send finds the same task) and see
 * its status as Torque has it now.
 */
function TorqueRow({
  w,
  desk,
  ws,
  id,
  task,
  kind,
  title,
  sent,
}: {
  w: WorkClient
  desk: Desk
  ws: string
  id: string
  task?: string
  kind: string
  title: string
  sent: () => void
}) {
  const [st, setSt] = useState<{ id: string; title?: string; status?: string } | null>(null)
  const [problem, setProblem] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!task) return setSt(null)
    let live = true
    w.torqueStatus(ws, id)
      .then((r) => {
        if (!live) return
        setSt(r.task)
        setProblem(r.problem ?? "")
      })
      .catch((e) => live && setProblem(message(e)))
    return () => {
      live = false
    }
  }, [w, ws, id, task])
  if (task)
    return (
      <div className="flex flex-wrap items-center gap-1.5 text-sm">
        <code className="text-xs">{task}</code>
        {st?.status && <Badge variant={st.status === "done" ? "secondary" : "outline"}>{label(st.status)}</Badge>}
        {problem && <span className="text-muted-foreground text-xs">{problem}</span>}
      </div>
    )
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Button
        variant="outline"
        size="sm"
        disabled={busy}
        onClick={async () => {
          const yes = await desk.confirm({
            title: `Send “${title}” to Torque?`,
            message: `One Torque task is made from this ${kind.toLowerCase()} (its text and a link back here), and its id is kept on it. Nothing else in Torque changes.`,
            confirmLabel: "Send",
          })
          if (!yes) return
          setBusy(true)
          try {
            await w.sendToTorque(ws, id)
            setProblem("")
          } catch (e) {
            setProblem(message(e))
          }
          setBusy(false)
          sent()
        }}
      >
        <Icon name="send" /> Send to Torque
      </Button>
      {problem && <span className="text-warning text-xs">{problem}</span>}
    </div>
  )
}
