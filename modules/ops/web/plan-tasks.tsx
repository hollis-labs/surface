import { CiteRef, CiteText, ObjectActionsMenu } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { age, message, type Plan, type Rollup, type Task } from "./api"
import { OpsChip, UnitNotesInline } from "./parts"

export function PlanTasks({
  roll,
  id,
  linkPlan,
}: {
  roll: Rollup
  id: string
  linkPlan: (plan: Plan) => Promise<void>
}) {
  const plans = roll.plans ?? []
  const planIDs = new Set(plans.map((p) => p.id))
  const other = roll.tasks.filter((t) => !t.plan_id || !planIDs.has(t.plan_id))
  return (
    <>
      {plans.map((plan) => {
        const tasks = roll.tasks.filter((t) => t.plan_id === plan.id)
        const phases = new Set(plan.phases.map((p) => p.id))
        const unassigned = tasks.filter((t) => !t.phase_id || !phases.has(t.phase_id))
        return (
          <section key={plan.id} aria-label={plan.title || plan.id} className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <CiteRef refId={{ kind: "torque.plan", canonical: plan.id }} label={plan.title || plan.id} />
              <Badge variant="secondary">{plan.status}</Badge>
              {plan.workstream !== id && <OpsChip id={plan.workstream} />}
              {plan.via && (
                <>
                  <span className="text-muted-foreground text-xs">
                    via <CiteRef refId={{ kind: "torque.task", canonical: plan.via }} />
                  </span>
                  <LinkPlan plan={plan} linkPlan={linkPlan} />
                </>
              )}
            </div>
            {plan.truncated && (
              <p className="text-warning text-xs">
                Only part of this plan's tasks could be shown. See the read problems above.
              </p>
            )}
            {plan.phases.map((phase) => {
              const rows = tasks.filter((t) => t.phase_id === phase.id)
              return (
                <section key={phase.id} aria-label={phase.name || phase.id} className="flex flex-col gap-1 pl-3">
                  <h4 className="flex items-center gap-2 font-medium">
                    {phase.name || phase.id}
                    <Badge variant="outline">{phase.state || "unknown"}</Badge>
                  </h4>
                  {rows.length ? (
                    <TaskTable tasks={rows} id={id} />
                  ) : (
                    <p className="text-muted-foreground text-xs">No tasks in this phase among the tasks read.</p>
                  )}
                </section>
              )
            })}
            {unassigned.length > 0 && (
              <section className="pl-3" aria-label="Other plan tasks">
                <h4 className="font-medium">Other plan tasks</h4>
                <p className="text-muted-foreground text-xs">No matching phase in this plan.</p>
                <TaskTable tasks={unassigned} id={id} />
              </section>
            )}
            {!tasks.length && !plan.phases.length && (
              <p className="text-muted-foreground text-xs">No phases or tasks returned.</p>
            )}
          </section>
        )
      })}
      {other.length > 0 && (
        <section aria-label="Linked and epic tasks">
          {plans.length > 0 && <h3 className="font-medium">Linked and epic tasks</h3>}
          <TaskTable tasks={other} id={id} />
        </section>
      )}
    </>
  )
}

function LinkPlan({ plan, linkPlan }: { plan: Plan; linkPlan: (plan: Plan) => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  return (
    <>
      <Button
        size="xs"
        variant="outline"
        disabled={busy}
        onClick={async () => {
          setBusy(true)
          setError("")
          try {
            await linkPlan(plan)
          } catch (e) {
            setError(message(e))
          } finally {
            setBusy(false)
          }
        }}
      >
        Link this plan
      </Button>
      {error && (
        <span role="alert" className="text-destructive text-xs">
          {error}
        </span>
      )}
    </>
  )
}

function TaskTable({ tasks, id }: { tasks: Task[]; id: string }) {
  return (
    <>
      {tasks.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Task</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>From</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead>Notes & Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasks.map((t) => (
              <TableRow key={t.id}>
                <TableCell className="max-w-96 whitespace-normal">
                  <CiteRef
                    refId={{ kind: "torque.task", canonical: t.id }}
                    label={t.title ? `${t.id} ${t.title}` : undefined}
                  />
                </TableCell>
                <TableCell>
                  <Badge variant={t.status === "doing" ? "default" : "outline"}>{t.status}</Badge>
                </TableCell>
                <TableCell className="text-xs">
                  {t.workstream !== id ? (
                    <OpsChip id={t.workstream} />
                  ) : t.via && t.via !== "link" ? (
                    <CiteText text={t.via} />
                  ) : (
                    "linked"
                  )}
                  {t.discovered_via && (
                    <span>
                      {" "}
                      via <CiteText text={t.discovered_via} />
                    </span>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground text-xs">{age(t.updated)}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-1">
                    <UnitNotesInline targetRef={t.id} compact />
                    <ObjectActionsMenu
                      obj={{
                        kind: "torque.task",
                        id: t.id,
                        title: t.title || t.id,
                        subtitle: t.status,
                      }}
                    />
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  )
}
