export type FieldType = "boolean" | "text" | "number" | "datetime" | "enum" | "uuid" | "json" | "fk" | "m2m";
export type BaseFieldType = Exclude<FieldType, "fk">;

// All entity ids are opaque masked tokens (11-char base64url), never raw numbers.
export type Id = string;

export interface Datasource {
  id: Id; name: string; driver: string; host: string; port: number;
  dbname: string; username: string; sslmode: string;
}

export type ValidationRuleType = "email" | "min_len" | "max_len" | "number" | "text";
export interface ValidationRule { type: ValidationRuleType; param?: number }

// ColumnFormatting is display-only config (never written to DB or CSV export).
export interface ColumnFormatting {
  enumColors?: Record<string, string> | null;
  number?: { thousands?: boolean; decimals?: number; prefix?: string } | null;
}

export interface ColumnDef {
  name: string; label: string; fieldType: FieldType;
  enumOptions: string[] | null;
  editable: boolean; required: boolean; visible: boolean;
  searchable: boolean; sortable: boolean; position: number;
  validations?: ValidationRule[] | null;
  isComputed?: boolean;
  computedFormula?: string;
  formatting?: ColumnFormatting | null;
  baseType?: BaseFieldType;
  fkTableDefId?: string; // masked token or "self"
  fkRefColumn?: string;
  fkDisplayColumns?: string[] | null;
  // many-to-many virtual columns
  m2mJunctionDefId?: string;
  m2mJunctionSrcCol?: string;
  m2mJunctionTgtCol?: string;
  m2mDisplayColumns?: string[] | null;
  m2mRefColumn?: string; // server-resolved source ref column (grid lookup key)
  m2mTargetRef?: string; // server-resolved target link value column
}

export interface Permissions {
  read: boolean; create: boolean; update: boolean; delete: boolean;
}

export type ViewMode = "grid" | "kanban" | "calendar";
export interface ViewConfig {
  kanbanBoardColumn?: string;
  kanbanDisplayColumn?: string;
  calendarStartColumn?: string;
  calendarEndColumn?: string | null;
}

export interface TableDef {
  id: Id; datasourceId: Id; schemaName: string; tableName: string;
  label: string; description?: string;
  sourceType?: "table" | "query";
  querySql?: string;
  keyColumns: string[]; pageSize: number;
  defaultSortCol: string; defaultSortDir: "ASC" | "DESC";
  defaultView?: ViewMode;
  viewConfig?: ViewConfig | null;
  hooks?: HooksConfig | null;
  groupId?: string; groupName?: string;
  permissions: Permissions;
}

export interface TableDefPayload extends TableDef { columns: ColumnDef[] }

export interface TableGroup { id: Id; name: string; position: number }

// SavedFilter is a per-user named filter set for one table (filters is the
// serialized ActiveFilter[] JSON produced by serializeFilters).
export interface SavedFilter { id: Id; name: string; filters: string; createdAt: string }

export interface LiveColumn {
  name: string; fieldType: FieldType; nullable: boolean;
  isPk: boolean; enumOptions: string[] | null;
}

// QueryIntrospectResult is the query-view wizard's validation response:
// resolved output columns plus expression columns that were dropped for
// lacking a stable alias.
export interface QueryIntrospectResult {
  columns: { name: string; fieldType: FieldType; nullable: boolean; isPk?: boolean; enumOptions?: string[] | null }[];
  dropped: string[];
}

export type Row = Record<string, unknown>;

export interface RowsRes {
  rows: Row[]; total: number; page: number; pageSize: number;
  rels?: Record<string, Record<string, Row>>;
  m2mRels?: Record<string, Record<string, Row[]>>;
}

export interface FkOptionsRes { rows: Row[]; total: number; page: number; pageSize: number }

export interface AuditEntry {
  id: Id; userId: Id; username: string; tableDefId: Id; action: string; rowPk: string;
  oldValues: Record<string, unknown> | null;
  newValues: Record<string, unknown> | null;
  createdAt: string;
}

export interface Me {
  username: string; isAdmin: boolean;
  manageDatasources: boolean; manageTables: boolean;
  viewAudit: boolean; viewOutbox: boolean;
  language?: string;
}

export interface User {
  id: Id; username: string; roleId: Id; roleName: string;
  disabled: boolean; isFirst: boolean;
}

export interface TableGrant {
  tableDefId: Id; canRead: boolean; canCreate: boolean;
  canUpdate: boolean; canDelete: boolean;
}

export interface Role {
  id: Id; name: string; isAdmin: boolean;
  manageDatasources: boolean; manageTables: boolean;
  viewAudit: boolean; viewOutbox: boolean;
  tables: TableGrant[]; userCount: number;
}

export type HookEvent =
  | "beforeCreate" | "afterCreate"
  | "beforeUpdate" | "afterUpdate"
  | "beforeDelete" | "afterDelete";

export interface HookAssignment {
  hook: string;
  config?: Record<string, unknown> | null;
  order: number;
}

export type HooksConfig = Partial<Record<HookEvent, HookAssignment[]>>;

export interface HooksListRes { hooks: string[] }

export interface OutboxEntry {
  id: Id; tableDefId: Id; event: string; hookName: string;
  status: "pending" | "done" | "dead"; attempts: number;
  nextRetryAt?: string; lastError?: string;
  createdAt: string; updatedAt: string;
}

export interface OutboxListRes { entries: OutboxEntry[]; total: number; page: number }
