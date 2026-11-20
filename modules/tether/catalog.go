package tether

import "github.com/hollis-labs/surface/module"

// entry is one gateway tool the agent is given: its level and the line it is listed with.
// The gateway's own definitions are long (written as guidance for coding agents), so the model
// is sent this line with a loose schema, and tether_describe returns the live definition the
// first time the agent needs a tool's arguments.
type entry struct {
	level module.Level
	line  string
}

// catalog is every gateway tool the agent may call, by name. A tool not listed here cannot be
// called through this module, whatever the gateway offers. Reading is free; anything that
// creates or changes a record asks first. Ported from Basalisk's list (internal/agent/tether.go).
var catalog = map[string]entry{
	// Tesseract: what is known.
	"tesseract_recall": {module.LevelRead, "Ranked recall across Tesseract memory and knowledge (and, opt-in, events and workspace) by query, namespace, tags or time."},
	"tesseract_get":    {module.LevelRead, "Fetch one current Tesseract entry by item_id, or by domain, namespace and key."},
	"tesseract_skills": {module.LevelRead, "Tesseract's own guide to its tools and to the shape of a write; read it before a first write."},
	"memory_write":     {module.LevelWrite, "Write a Tesseract memory entry (a decision, follow-up, limitation, preference) in a project or app namespace."},
	"knowledge_write":  {module.LevelWrite, "Write a Tesseract knowledge entry (a reference, design, investigation, playbook) in a project or app namespace."},
	"workspace_write":  {module.LevelWrite, "Write a Tesseract workspace draft (mutable, for review before it is promoted)."},
	// Torque: what is to be done.
	"torque_project_list":      {module.LevelRead, "List Torque projects."},
	"torque_project_get":       {module.LevelRead, "Get one Torque project by id."},
	"torque_epic_list":         {module.LevelRead, "List Torque epics, optionally by project."},
	"torque_epic_get":          {module.LevelRead, "Get one Torque epic by id."},
	"torque_sprint_list":       {module.LevelRead, "List Torque sprints."},
	"torque_sprint_get":        {module.LevelRead, "Get one Torque sprint by id."},
	"torque_task_list":         {module.LevelRead, "List Torque tasks by status, priority, tags, project, parent or free-text search."},
	"torque_task_get":          {module.LevelRead, "Get one Torque task by id, with its newest comments."},
	"torque_comment_list":      {module.LevelRead, "List the comments on a Torque task, project, epic or sprint."},
	"torque_task_subtodo_list": {module.LevelRead, "List a Torque task's checklist."},
	"torque_task_create":       {module.LevelWrite, "Create a Torque task."},
	"torque_task_update":       {module.LevelWrite, "Change fields of a Torque task."},
	"torque_task_transition":   {module.LevelWrite, "Move a Torque task to another status, with an optional comment."},
	"torque_comment_add":       {module.LevelWrite, "Add a comment to a Torque task, project, epic or sprint."},
	"torque_task_subtodo_add":  {module.LevelWrite, "Add an item to a Torque task's checklist."},
	"torque_task_subtodo_done": {module.LevelWrite, "Tick an item on a Torque task's checklist."},
	// Caster: named, revisioned text and refs.
	"caster_head":   {module.LevelRead, "The newest revision of a Caster document."},
	"caster_get":    {module.LevelRead, "Read a Caster document (or one revision of it)."},
	"caster_ls":     {module.LevelRead, "List Caster documents under a namespace."},
	"caster_log":    {module.LevelRead, "A Caster document's revision history."},
	"caster_search": {module.LevelRead, "Search Caster documents."},
	"caster_put":    {module.LevelWrite, "Write a new revision of a Caster document."},
	// Cerberus: what runs where.
	"cerberus_health":                {module.LevelRead, "Cerberus's own health."},
	"cerberus_portfolio_health":      {module.LevelRead, "Health across the portfolio's services."},
	"cerberus_project_list":          {module.LevelRead, "List the projects Cerberus knows."},
	"cerberus_resource_list":         {module.LevelRead, "List Cerberus resources (services, processes) by project, connector or tag."},
	"cerberus_resource_status":       {module.LevelRead, "A resource's runtime status and the recommended next action."},
	"cerberus_resource_inspect":      {module.LevelRead, "A resource's runtime details, install paths and log paths."},
	"cerberus_resource_logs":         {module.LevelRead, "A resource's recent logs."},
	"cerberus_resource_doctor":       {module.LevelRead, "Diagnose a resource."},
	"cerberus_host_list":             {module.LevelRead, "List the hosts Cerberus manages."},
	"cerberus_host_get":              {module.LevelRead, "One host's record."},
	"cerberus_host_status":           {module.LevelRead, "One host's status."},
	"cerberus_host_capacity":         {module.LevelRead, "One host's capacity (CPU, memory, disk)."},
	"cerberus_host_logs":             {module.LevelRead, "One host's recent logs."},
	"cerberus_schedule_list":         {module.LevelRead, "List Cerberus schedules."},
	"cerberus_resource_ensure_fresh": {module.LevelWrite, "Bring a resource up to date (build, sync, apply as needed)."},
	"cerberus_resource_reload":       {module.LevelWrite, "Reload a running resource."},
	"cerberus_resource_deploy":       {module.LevelWrite, "Build, sync and apply a resource from its source."},
	// Tether: the agents themselves.
	"tether_health":          {module.LevelRead, "Tether's health."},
	"tether_gateway_status":  {module.LevelRead, "The Tether gateway's status and its upstream servers."},
	"tether_session_list":    {module.LevelRead, "List agent sessions: who is running."},
	"tether_session_get":     {module.LevelRead, "One agent session."},
	"tether_session_health":  {module.LevelRead, "One agent session's health."},
	"tether_status_get":      {module.LevelRead, "An agent's or seat's posted status."},
	"tether_message_trace":   {module.LevelRead, "Where a message went: its delivery trace."},
	"tether_registry_lookup": {module.LevelRead, "Look up an agent or address in Tether's registry."},
	// The Registry: the records of what exists and how they relate.
	"registry_kinds_list":      {module.LevelRead, "List the Registry's record kinds."},
	"registry_schema_get":      {module.LevelRead, "A Registry kind's schema."},
	"registry_record_list":     {module.LevelRead, "List Registry records of a kind."},
	"registry_record_get":      {module.LevelRead, "One Registry record."},
	"registry_record_search":   {module.LevelRead, "Search Registry records."},
	"registry_relation_list":   {module.LevelRead, "List a Registry record's relations."},
	"registry_revision_list":   {module.LevelRead, "A Registry record's revisions."},
	"registry_revision_get":    {module.LevelRead, "One revision of a Registry record."},
	"registry_snapshot_get":    {module.LevelRead, "A Registry snapshot."},
	"registry_review_list":     {module.LevelRead, "List Registry changes waiting for review."},
	"registry_diagnostics":     {module.LevelRead, "The Registry's diagnostics."},
	"registry_tool_resolve":    {module.LevelRead, "Resolve a tool to its Registry record."},
	"registry_tool_classify":   {module.LevelRead, "A tool's classification in the Registry."},
	"registry_record_create":   {module.LevelWrite, "Create a Registry record."},
	"registry_record_update":   {module.LevelWrite, "Change a Registry record."},
	"registry_record_verify":   {module.LevelWrite, "Mark a Registry record verified."},
	"registry_record_archive":  {module.LevelWrite, "Archive a Registry record."},
	"registry_relation_create": {module.LevelWrite, "Relate two Registry records."},
	"registry_relation_remove": {module.LevelWrite, "Remove a relation between Registry records."},
	"registry_review_decide":   {module.LevelWrite, "Decide a Registry review."},
	"registry_tool_propose":    {module.LevelWrite, "Propose a tool to the Registry."},
	"registry_record_delete":   {module.LevelConfirm, "Delete a Registry record. Asks every time."},
	// Publishing and deciding: what turns drafts into the published Registry.
	"registry_changes_list":                 {module.LevelRead, "List recent changes to the Registry."},
	"registry_references_report":            {module.LevelRead, "Report what references a Registry record."},
	"registry_publication_policy_get":       {module.LevelRead, "The Registry's publication policy."},
	"registry_publication_approval_get":     {module.LevelRead, "One Registry publication approval."},
	"registry_publication_approval_prepare": {module.LevelWrite, "Prepare a Registry publication for approval."},
	"registry_relation_decide":              {module.LevelConfirm, "Accept or reject a proposed Registry relation. Asks every time."},
	"registry_publication_approval_decide":  {module.LevelConfirm, "Approve or reject a Registry publication. Asks every time."},
	"registry_publication_publish":          {module.LevelConfirm, "Publish approved Registry drafts. Asks every time."},
}

// mailCatalog is the gateway's mail, given to the agent only when it has an address of its own
// (the agent_address setting). Whatever the model passes, mail is sent from that address and
// read from that address's box: the agent cannot write as the owner or anyone else, and cannot
// read another's mail.
var mailCatalog = map[string]entry{
	"tether_registry_search":   {module.LevelRead, "Find agents and people to write to in Tether's registry."},
	"tether_message_list":      {module.LevelRead, "List the agent's own Tether mail."},
	"tether_message_get":       {module.LevelRead, "Read one message from the agent's own mailbox."},
	"tether_message_mark_read": {module.LevelAct, "Mark a message in the agent's own mailbox read."},
	"tether_message_notify":    {module.LevelWrite, "Send a message from the agent's own address, and wake the recipient if it is running."},
}
