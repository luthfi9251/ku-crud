export type FieldType = "boolean" | "text" | "number" | "datetime" | "enum";

// All entity ids are opaque masked tokens (11-char base64url), never raw numbers.
export type Id = string;

export interface Datasource {
  id: Id; name: string; host: string; port: number;
  dbname: string; username: string; sslmode: string;
}

export interface ColumnDef {
  name: string; label: string; fieldType: FieldType;
  enumOptions: string[] | null;
  editable: boolean; required: boolean; visible: boolean;
  searchable: boolean; sortable: boolean; position: number;
}

export interface Permissions {
  read: boolean; create: boolean; update: boolean; delete: boolean;
}

export interface TableDef {
  id: Id; datasourceId: Id; schemaName: string; tableName: string;
  label: string; keyColumns: string[]; pageSize: number;
  permissions: Permissions;
}

export interface TableDefPayload extends TableDef { columns: ColumnDef[] }

export interface LiveColumn {
  name: string; fieldType: FieldType; nullable: boolean;
  isPk: boolean; enumOptions: string[] | null;
}

export type Row = Record<string, unknown>;

export interface RowsRes { rows: Row[]; total: number; page: number; pageSize: number }

export interface AuditEntry {
  id: Id; userId: Id; username: string; tableDefId: Id; action: string; rowPk: string;
  oldValues: Record<string, unknown> | null;
  newValues: Record<string, unknown> | null;
  createdAt: string;
}

export interface Me {
  username: string; isAdmin: boolean; platformManage: boolean;
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
  id: Id; name: string; isAdmin: boolean; platformManage: boolean;
  tables: TableGrant[]; userCount: number;
}
