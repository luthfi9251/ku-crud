import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { BookOpen, Code2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";

type Audience = "business" | "developer";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm text-muted-foreground leading-relaxed">
        {children}
      </CardContent>
    </Card>
  );
}

function Li({ children }: { children: React.ReactNode }) {
  return <li className="list-disc ml-4 marker:text-primary/60">{children}</li>;
}

function K({ children }: { children: React.ReactNode }) {
  return <Badge variant="secondary" className="font-mono text-[11px]">{children}</Badge>;
}

function BusinessDocs() {
  return (
    <div className="space-y-6">
      <Section title="Getting started">
        <ul className="space-y-2">
          <Li>Log in with the account your administrator gave you. If you enter a wrong password too many times, the login is temporarily blocked (5 failures within 15 minutes).</Li>
          <Li>You can switch the interface language (Indonesian / English) from the <K>Language</K> selector at the bottom of the sidebar — your choice is saved to your profile.</Li>
          <Li>The sidebar lists the tables you have access to, optionally organized into groups. Click a table to open its data page.</Li>
        </ul>
      </Section>

      <Section title="Who sees what">
        <ul className="space-y-2">
          <Li>The menus you see depend on what your administrator granted your role: some people also see <K>Datasources</K>, <K>Definitions Transfer</K>, <K>Audit Trail</K> or <K>Hook Outbox</K> — if a menu is missing, your role simply does not include it.</Li>
          <Li>When an action fails, the message explains what happened in plain words. <K>Technical detail</K> unfolds the raw error for your administrator.</Li>
        </ul>
      </Section>

      <Section title="Working with the grid">
        <ul className="space-y-2">
          <Li>Type in the search box to filter rows; click a column header to sort by that column.</Li>
          <Li>Use the pagination controls at the bottom of the grid to move between pages. The page size is set per table by the administrator.</Li>
          <Li><K>Group by</K> groups rows by a column you choose, with a header per group showing the group value and the number of rows in it.</Li>
        </ul>
      </Section>

      <Section title="Filtering data">
        <ul className="space-y-2">
          <Li>Each column can have its own filter with operators that match the column type: text columns offer contains / equals / not equals, number and date columns offer ranges and comparisons, and some columns accept a list of values ("in list").</Li>
          <Li>Several column filters combine at once (AND logic). Active filters appear as chips above the grid — click a chip to remove that filter.</Li>
          <Li><K>Saved filters</K>: save the current filter combination under a name and reload it later from the dropdown on the data page. Saved filters are private — only you can see yours.</Li>
          <Li>Filters stay active when you export to CSV, so the file matches what you see.</Li>
        </ul>
      </Section>

      <Section title="Different views: grid, kanban, calendar">
        <ul className="space-y-2">
          <Li>A table can be displayed as a <K>Grid</K>, a <K>Kanban</K> board, or a <K>Calendar</K>. Switch views from the data page; the default is chosen by the administrator.</Li>
          <Li><b>Kanban</b>: cards sit in one column per value of the board's status column. Drag a card to another column to update that field — this requires update permission and every drag is recorded in the audit trail.</Li>
          <Li><b>Calendar</b>: rows appear as events on their date. Click an event to open the row details; click an empty day to create a new row with that date pre-filled.</Li>
        </ul>
      </Section>

      <Section title="Adding and editing rows">
        <ul className="space-y-2">
          <Li>Use the <K>New row</K> button to create a record and the row menu to view, edit, or delete one. Buttons for actions you don't have permission for are hidden.</Li>
          <Li>Your administrator can add extra buttons next to the row actions (for example <K>Send invoice</K>). Clicking one asks for confirmation when needed, then shows a short message with the result — every run is recorded in the audit trail.</Li>
          <Li>Fields that link to another table open a searchable picker — search for the record you want and select it. If it doesn't exist yet, create it first on that table's own page.</Li>
          <Li>Fields that connect to many records (many-to-many) are multi-select pickers: check all the records you want to link.</Li>
          <Li>JSON fields are displayed nicely formatted, and the content is checked to be valid JSON before saving.</Li>
          <Li>Some fields may have rules (for example "must be a valid email" or "maximum 30 characters"). If a value breaks a rule you'll see a clear message under the field — fix the value and save again.</Li>
        </ul>
      </Section>

      <Section title="Import & export, bulk actions">
        <ul className="space-y-2">
          <Li><K>Export CSV</K> downloads exactly what the grid shows: current search, filters, and sorting are applied, relation fields are included as their display values, up to 100,000 rows.</Li>
          <Li><K>Import CSV</K>: upload a file (comma, semicolon, or tab separated — detected automatically, up to 5 MB or 10,000 rows). Check the column mapping and the per-row validation preview, then choose to insert only the valid rows or all of them.</Li>
          <Li>Select multiple rows with the checkboxes and use the bulk delete button to remove them all in one confirmed action. Rows that are referenced elsewhere are reported per row.</Li>
        </ul>
      </Section>

      <Section title="The audit trail">
        <ul className="space-y-2">
          <Li>Every create, update, and delete is recorded — including CSV imports, bulk deletes, and kanban card drags — with who did it, when, and the values before and after.</Li>
          <Li>The audit trail page is visible only to users who were granted access to it.</Li>
        </ul>
      </Section>

      <Section title="Messages you might see">
        <ul className="space-y-2">
          <Li><b>Red "schema changed" banner</b>: the table's structure in the database no longer matches its definition in Ku-CRUD. An administrator must re-sync the definition before the data page works reliably again.</Li>
          <Li><b>"Row is referenced" error</b>: the record is linked from another table, so it can't be deleted until the links are removed.</Li>
          <Li><b>Validation error under a field</b>: the value breaks a rule defined for that column (email format, length limits, numbers only, letters only). Correct the value and try again.</Li>
        </ul>
      </Section>
    </div>
  );
}

function DeveloperDocs() {
  const t = useT();
  return (
    <div className="space-y-6">
      <Section title="Datasources">
        <ul className="space-y-2">
          <Li>Register a Postgres or MySQL database under <K>Datasources</K>: driver, host, port, database, user, password, and sslmode. Use <K>Test connection</K> before saving.</Li>
          <Li>Passwords are stored encrypted at rest (AES-256-GCM) in the SQLite metadata file — still protect the file itself with filesystem permissions.</Li>
          <Li>Connections are opened on demand, not kept alive permanently.</Li>
        </ul>
      </Section>

      <Section title="Defining a table">
        <ul className="space-y-2">
          <Li>The 3-step wizard: pick a datasource → pick an introspected table (columns, types, keys, and enums are read from the live schema) → tune the definition.</Li>
          <Li>Per column you can set: label, editable, visible, searchable, sortable, type, validation rules, and formatting.</Li>
          <Li>Per table you can set: display label, page size, and default sort (column + direction) that every user starts with.</Li>
          <Li>Ku-CRUD never runs DDL. It only reads <K>information_schema</K> — all structure changes stay your responsibility.</Li>
          <Li>Supported column types: boolean, number, text, datetime, enum, uuid, json, plus the fk and m2m relation types. Arrays and binary columns are excluded.</Li>
        </ul>
      </Section>

      <Section title="Keys">
        <ul className="space-y-2">
          <Li>Key columns only drive the WHERE predicate for update and delete. Choose a single column or a composite key.</Li>
          <Li>They don't have to be the database's real primary key — but at least one key column is required per definition.</Li>
        </ul>
      </Section>

      <Section title="Relations (fk and m2m)">
        <ul className="space-y-2">
          <Li>An <K>fk</K> column points at another table definition — same or another datasource, self-reference allowed. You choose the target table plus which of its fields display in grids and forms.</Li>
          <Li>Forms pick related records through a searchable modal. Related records are edited on their own table's page (single source of truth) — the fk field itself is not edited inline.</Li>
          <Li>An <K>m2m</K> virtual column models a junction table: a defined table with exactly two fk columns, one to this table and one to the target. Grids show joined display values; forms manage links with a multi-select picker.</Li>
          <Li>Managing m2m links requires create + delete grants on the junction table. Deleting a row that other defined tables reference is blocked with a conflict message; database FK violations surface the same way.</Li>
        </ul>
      </Section>

      <Section title="Validation rules">
        <ul className="space-y-2">
          <Li>Per column, optionally define rules: email format, min length, max length, number-only, text-only. A column can combine several rules.</Li>
          <Li>Rules run server-side on create and update, and are applied per row in the CSV import preview — invalid rows are marked before anything is inserted.</Li>
        </ul>
      </Section>

      <Section title="Computed columns">
        <ul className="space-y-2">
          <Li>Definition-level virtual columns evaluated server-side from an allowlisted formula: arithmetic (+, -, *, /) between number columns, or concatenation of text columns, with optional constants. No free-form expressions.</Li>
          <Li>They are never stored in the database and appear in the grid, row detail, and CSV export.</Li>
          <Li>They cannot be filtered, sorted, or searched server-side (they don't exist in the database) — the UI marks this.</Li>
        </ul>
      </Section>

      <Section title="Formatting">
        <ul className="space-y-2">
          <Li>Formatting is presentation-only — stored values are never modified.</Li>
          <Li><K>Enum</K>: badge with a color per value, chosen from the provided palette.</Li>
          <Li><K>Number</K>: thousands separator, decimal places, optional currency prefix.</Li>
          <Li><K>Datetime</K>: locale-aware, human-friendly display.</Li>
        </ul>
      </Section>

      <Section title="Views">
        <ul className="space-y-2">
          <Li>Per table, choose the default view: grid, kanban, or calendar. Users can still switch views on the data page.</Li>
          <Li>Kanban needs one enum column as the board column; each value becomes a board column. Drag-drop updates the field, subject to update grants, and is audited.</Li>
          <Li>Calendar needs a datetime column as the event date, optionally a second one as end date. Clicking a day opens the create form with the date pre-filled; clicking an event opens the row.</Li>
        </ul>
      </Section>

      <Section title={t("docs.hooks.title")}>
        <ul className="space-y-2">
          <Li>{t("docs.hooks.what")}</Li>
          <Li>{t("docs.hooks.events")}</Li>
          <Li>{t("docs.hooks.before")}</Li>
          <Li>{t("docs.hooks.after")}</Li>
          <Li>{t("docs.hooks.outbox")}</Li>
          <Li>{t("docs.hooks.devflow")}</Li>
          <Li>{t("docs.hooks.rename")}</Li>
          <Li>{t("docs.hooks.missing")}</Li>
        </ul>
      </Section>

      <Section title={t("docs.actions.title")}>
        <ul className="space-y-2">
          <Li>{t("docs.actions.hide")}</Li>
          <Li>{t("docs.actions.custom")}</Li>
        </ul>
      </Section>

      <Section title="Schema drift & re-sync">
        <ul className="space-y-2">
          <Li>Every visit to a data page verifies the live schema against the stored definition.</Li>
          <Li>On drift the UI shows a red banner listing missing, added, and type-changed columns, with a one-click <K>Re-sync</K> to update the definition.</Li>
        </ul>
      </Section>

      <Section title="Moving definitions between instances">
        <ul className="space-y-2">
          <Li><K>Definitions Transfer</K> exports datasources and table definitions to a single JSON file — datasource passwords are never included.</Li>
          <Li>On import you get a preview of new, duplicate, and conflicting definitions and choose what to apply; relations to other definitions are remapped.</Li>
          <Li>Re-enter datasource passwords on the target instance after importing.</Li>
        </ul>
      </Section>

      <Section title="Users & roles">
        <ul className="space-y-2">
          <Li>The first user is the immutable builtin <K>Admin</K> with every permission. If you ever lock yourself out, the <K>seed-admin</K> CLI tool is the recovery path.</Li>
          <Li>Custom roles combine a Platform Management bundle (datasources, table definitions, audit trail) with independent read / create / update / delete grants per table. A user holds exactly one role.</Li>
          <Li>User and role management is admin-only. Disabled users are rejected at login and on every request.</Li>
          <Li>fk display values and the record picker resolve only for users with read access to the target table — others see raw column values.</Li>
        </ul>
      </Section>

      <Section title="Runtime security behavior">
        <ul className="space-y-2">
          <Li>Login and first-run setup are rate limited: 5 failures per username+IP within 15 minutes returns HTTP 429.</Li>
          <Li>Sessions: signed HttpOnly cookie, 24-hour expiry, SameSite=Lax.</Li>
          <Li>Every entity id crossing the API is an opaque masked token — numeric ids are rejected.</Li>
          <Li>All SQL is fully parameterized with strict identifier validation against the stored definition; search input has LIKE wildcards escaped; sort and filter columns are checked against the definition.</Li>
        </ul>
      </Section>
    </div>
  );
}

export default function Docs() {
  const [audience, setAudience] = useState<Audience>("business");

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div className="flex items-center gap-2">
        <Button
          variant={audience === "business" ? "default" : "outline"}
          size="sm"
          onClick={() => setAudience("business")}
          className={cn("gap-1.5", audience === "business" && "pointer-events-none")}
        >
          <BookOpen className="h-3.5 w-3.5" />
          For Business Users
        </Button>
        <Button
          variant={audience === "developer" ? "default" : "outline"}
          size="sm"
          onClick={() => setAudience("developer")}
          className={cn("gap-1.5", audience === "developer" && "pointer-events-none")}
        >
          <Code2 className="h-3.5 w-3.5" />
          For Developers
        </Button>
      </div>

      <p className="text-sm text-muted-foreground">
        Ku-CRUD gives your existing Postgres and MySQL tables a ready-to-use
        web interface — without touching the database structure.
      </p>

      {audience === "business" ? <BusinessDocs /> : <DeveloperDocs />}
    </div>
  );
}
