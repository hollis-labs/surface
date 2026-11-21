import { CiteText, Icon } from "@hollis-labs/surface-page/sdk"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { age, type Entry, time } from "./api"
import { OpsChip } from "./parts"

export const SOURCE_ICON: Record<string, string> = {
  torque: "list-checks",
  comment: "message-square",
  status: "activity",
  ops: "radar",
}
export const SOURCE_LABEL: Record<string, string> = {
  torque: "Torque",
  comment: "Status comments",
  status: "Status stream",
  ops: "Ops moves",
}

export function ActivityRow({ e, id, open, toggle }: { e: Entry; id: string; open: boolean; toggle: () => void }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex min-w-0 items-start gap-2">
        <span className="w-14 shrink-0 text-muted-foreground text-xs" title={time(e.at)}>
          {age(e.at)}
        </span>
        <Icon
          name={SOURCE_ICON[e.source] ?? "dot"}
          size={14}
          className="mt-0.5 shrink-0 text-muted-foreground"
          aria-label={SOURCE_LABEL[e.source]}
        />
        <span className="min-w-0 flex-1">
          <CiteText text={e.summary} />
          {e.source === "status" && (
            <Badge
              variant="outline"
              className="ml-1.5"
              title="Shown as the writer claimed it: no writer is verified yet"
            >
              {e.verified ? "verified" : "claimed"}
            </Badge>
          )}
        </span>
        {e.workstream !== id && <OpsChip id={e.workstream} />}
        <Button size="icon-xs" variant="ghost" aria-label={open ? "Less" : "More"} onClick={toggle}>
          <Icon name={open ? "chevron-up" : "chevron-down"} />
        </Button>
      </div>
      {open && (
        <dl className="ml-16 grid grid-cols-[6rem_1fr] gap-x-2 text-xs">
          <dt className="text-muted-foreground">At</dt>
          <dd>{time(e.at)}</dd>
          <dt className="text-muted-foreground">Source</dt>
          <dd>{SOURCE_LABEL[e.source]}</dd>
          {e.state && (
            <>
              <dt className="text-muted-foreground">State</dt>
              <dd>{e.state}</dd>
            </>
          )}
          {e.task && (
            <>
              <dt className="text-muted-foreground">Task</dt>
              <dd>
                <CiteText text={e.task} />
              </dd>
            </>
          )}
          {e.subject_urn && (
            <>
              <dt className="text-muted-foreground">Subject</dt>
              <dd>
                <CiteText text={e.subject_urn} />
              </dd>
            </>
          )}
          <dt className="text-muted-foreground">Workstream</dt>
          <dd>
            <OpsChip id={e.workstream} />
          </dd>
        </dl>
      )}
    </div>
  )
}
