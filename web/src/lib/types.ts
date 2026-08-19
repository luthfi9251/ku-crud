export type FieldType = "boolean" | "text" | "number" | "datetime" | "enum";

export interface Datasource {
  id: number; name: string; host: string; port: number;
  dbname: string; username: string; sslmode: string;
}

export interface ColumnDef {
  name: string; label: string; fieldType: FieldType;
  enumOptions: string[] | null;
  editable: boolean; required: boolean; visible: boolean;
  searchable: boolean; sortable: boolean; position: number;
}

export interface TableDef {
  id: number; datasourceId: number; schemaName: string; tableName: string;
  label: string; pkColumn: string; pageSize: number;
}

export interface TableDefPayload extends TableDef { columns: ColumnDef[] }

export interface LiveColumn {
  name: string; fieldType: FieldType; nullable: boolean;
  isPk: boolean; enumOptions: string[] | null;
}

export type Row = Record<string, unknown>;

export interface RowsRes { rows: Row[]; total: number; page: number; pageSize: number }

export interface AuditEntry {
  id: number; userId: number; tableDefId: number; action: string; rowPk: string;
  oldValues: Record<string, unknown> | null;
  newValues: Record<string, unknown> | null;
  createdAt: string;
}
