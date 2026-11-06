import type { ColumnDef, RowData, Table } from "@tanstack/vue-table"
import {
  createPaginatedRowModel,
  rowPaginationFeature,
  tableFeatures,
} from "@tanstack/vue-table"

/** The table features that DataTable uses. */
export const dataTableFeatures = tableFeatures({
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
})

export type DataTableFeatures = typeof dataTableFeatures

/** A column of a DataTable. */
export type DataTableColumn<TData extends RowData, TValue = unknown> = ColumnDef<DataTableFeatures, TData, TValue>

/** The table instance behind a DataTable. */
export type DataTableInstance<TData extends RowData> = Table<DataTableFeatures, TData>
